import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../client", () => ({
  api: { get: vi.fn() },
}));

const { api } = await import("../client");
const { listDiscoveredDevices } = await import("./devices");
const { buildUrl } = await vi.importActual<typeof import("../client")>(
  "../client",
);

describe("listDiscoveredDevices", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // 后端把整个 IMEI 探测关在 with_imei 后面（internal/api/device_discovery.go）。
  // 漏掉它不会报错——只会让每台设备的 IMEI 都是空的，前端据此判 degraded、
  // 显示「身份不可确立」并禁用添加按钮，于是任何模组都加不进来。
  // 真机上就是这么坏的：EC20 驱动、cdc-wdm、AT 口全正常，UI 里却加不了。
  it("必须带 with_imei=1，否则拿回来的设备全是无身份的", async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: [],
      meta: {},
      requestId: "req-1",
    });

    await listDiscoveredDevices();

    expect(api.get).toHaveBeenCalledWith(
      "/devices/discovered",
      expect.objectContaining({ query: { with_imei: "1" } }),
    );
  });

  // 探测要实打实打串口和 QMI，默认超时不够用。
  it("保留加长超时，探测比普通请求慢得多", async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: [],
      meta: {},
      requestId: "req-1",
    });

    await listDiscoveredDevices();

    expect(api.get).toHaveBeenCalledWith(
      "/devices/discovered",
      expect.objectContaining({ timeoutMs: 60_000 }),
    );
  });

  // query 走 client 的 URLSearchParams 分支，不是拼在 path 里——
  // 拼在 path 里的话，将来谁再传一个 query 就会拼出两个 `?`。
  it("参数经 buildUrl 正确落到查询串上", () => {
    expect(buildUrl("/devices/discovered", { with_imei: "1" })).toBe(
      "/api/devices/discovered?with_imei=1",
    );
  });
});
