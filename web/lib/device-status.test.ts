import { describe, expect, it } from "vitest";
import { phaseMeta, signalLevel, summarizeDeviceStatus } from "./device-status";
import type { DeviceOverview } from "@/types/device";

/**
 * 状态展示是 9 个 phase × 8 个布尔位的组合，也是现场验证时最容易被质疑
 * "这灯为什么是这个颜色"的地方。判定顺序本身就是需求，用测试把它钉住。
 */

function overview(patch: Partial<DeviceOverview> = {}): DeviceOverview {
  return {
    id: "dev1",
    name: "设备 1",
    lifecycle_phase: "online",
    healthy: true,
    control_online: true,
    worker_running: true,
    physical_present: true,
    radio_registered: true,
    data_connected: true,
    network_connected: true,
    network_enabled: true,
    sms_enabled: true,
    vowifi_enabled: false,
    vowifi_active: false,
    public_ip: "",
    private_ip: "",
    modem: {},
    ...patch,
  } as DeviceOverview;
}

describe("phaseMeta", () => {
  it("未知或缺失的 phase 落到 unknown 而不是崩溃", () => {
    expect(phaseMeta(undefined).tone).toBe("neutral");
    expect(phaseMeta("这个阶段不存在").tone).toBe("neutral");
  });

  it("已知 phase 有各自的语调", () => {
    expect(phaseMeta("online").tone).toBe("ok");
    expect(phaseMeta("degraded").tone).toBe("warn");
  });
});

describe("summarizeDeviceStatus 判定顺序", () => {
  // 过渡态优先：用户首先需要知道"正在做什么"，此时不该报警。
  it("过渡态优先于一切，且不报警", () => {
    const s = summarizeDeviceStatus(
      overview({
        lifecycle_phase: "worker_starting",
        healthy: false,
        radio_registered: false,
      }),
    );
    expect(s.transient).toBe(true);
    expect(s.tone).not.toBe("danger");
  });

  it("过渡态展示后端给的原因", () => {
    const s = summarizeDeviceStatus(
      overview({ lifecycle_phase: "worker_starting", lifecycle_reason: "等待 QMI 就绪" }),
    );
    expect(s.detail).toBe("等待 QMI 就绪");
  });

  // 离线要区分"硬件在位但没起来"和"硬件根本不在"——两者的处置完全不同。
  it("离线时区分硬件是否在位", () => {
    expect(
      summarizeDeviceStatus(
        overview({ lifecycle_phase: "offline", physical_present: true }),
      ).detail,
    ).toContain("硬件在位");
    expect(
      summarizeDeviceStatus(
        overview({ lifecycle_phase: "offline", physical_present: false }),
      ).detail,
    ).toContain("硬件不在位");
  });

  it("healthy=false 即降级，并列出具体问题", () => {
    const s = summarizeDeviceStatus(
      overview({ healthy: false, control_online: false, worker_running: false }),
    );
    expect(s.label).toBe("降级");
    expect(s.detail).toContain("控制通道断开");
    expect(s.detail).toContain("工作线程未运行");
  });

  it("降级时后端给了原因就用后端的", () => {
    expect(
      summarizeDeviceStatus(
        overview({ healthy: false, lifecycle_reason: "eUICC 无响应" }),
      ).detail,
    ).toBe("eUICC 无响应");
  });

  // 在线且健康之后才细分：没注册网络 ≠ 没联网，前者更靠前。
  it("在线但未注册网络", () => {
    const s = summarizeDeviceStatus(overview({ radio_registered: false }));
    expect(s.label).toBe("未注册");
    expect(s.tone).toBe("warn");
  });

  it("已驻 LTE 但未完成数据附着时单独提示", () => {
    const s = summarizeDeviceStatus(
      overview({
        radio_registered: false,
        network_enabled: true,
        data_connected: false,
        modem: { cell_camped: true, sim_inserted: true } as DeviceOverview["modem"],
      }),
    );
    expect(s.label).toBe("已驻 LTE");
    expect(s.tone).toBe("warn");
    expect(s.detail).toContain("数据业务尚未附着");
  });

  it("已看到小区但 SIM 未就绪时明确提示卡状态", () => {
    const s = summarizeDeviceStatus(
      overview({
        radio_registered: false,
        modem: { cell_camped: true, sim_inserted: false } as DeviceOverview["modem"],
      }),
    );
    expect(s.label).toBe("已驻 LTE");
    expect(s.detail).toContain("SIM 尚未就绪");
  });

  it("已注册但数据未连接", () => {
    const s = summarizeDeviceStatus(
      overview({ network_enabled: true, data_connected: false }),
    );
    expect(s.label).toBe("未联网");
    expect(s.tone).toBe("warn");
  });

  // 用户主动关掉数据时不该报警——那是意图，不是故障。
  it("数据开关关闭时不算未联网", () => {
    const s = summarizeDeviceStatus(
      overview({ network_enabled: false, data_connected: false }),
    );
    expect(s.label).not.toBe("未联网");
    expect(s.tone).toBe("ok");
  });

  it("一切正常时展示出口 IP", () => {
    expect(summarizeDeviceStatus(overview({ public_ip: "1.2.3.4" })).detail).toBe(
      "出口 IP 1.2.3.4",
    );
  });
});

describe("signalLevel", () => {
  // RSRP 为 0 或缺失是"没读到"，不是"信号极强"——按信号强度算 0 会是满格。
  it("0 与缺失都算无信号", () => {
    expect(signalLevel(0).label).toBe("无信号");
    expect(signalLevel(undefined).label).toBe("无信号");
  });

  it("按 RSRP 分级", () => {
    expect(signalLevel(-80).label).toBe("优");
    expect(signalLevel(-95).label).toBe("良");
    expect(signalLevel(-105).label).toBe("中");
    expect(signalLevel(-120).label).toBe("弱");
  });

  it("分级边界取闭区间", () => {
    expect(signalLevel(-85).label).toBe("优");
    expect(signalLevel(-100).label).toBe("良");
    expect(signalLevel(-110).label).toBe("中");
  });

  it("弱信号是 danger，其余不是", () => {
    expect(signalLevel(-120).tone).toBe("danger");
    expect(signalLevel(-105).tone).toBe("warn");
    expect(signalLevel(-80).tone).toBe("ok");
  });
});
