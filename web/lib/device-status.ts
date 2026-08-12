/**
 * 设备状态展示映射。
 *
 * 后端把设备状态拆成两部分，前端必须组合后才能给出可解释的结论：
 *  - lifecycle_phase：9 个阶段的状态机（internal/device/lifecycle.go）
 *  - 8 个彼此独立的布尔位：running / healthy / control_online / physical_present /
 *    worker_running / data_connected / radio_registered / network_connected
 *
 * 只看 phase 会漏掉「online 但没联网」，只看布尔位则无法解释「为什么在重启」。
 */

import type { DeviceOverview, LifecyclePhase } from "@/types/device";

export type StatusTone = "ok" | "warn" | "danger" | "neutral" | "progress";

interface PhaseMeta {
  label: string;
  tone: StatusTone;
  /** 是否属于过渡态：过渡态应显示进度感，且不应报警 */
  transient: boolean;
  hint: string;
}

const PHASE_META: Record<LifecyclePhase, PhaseMeta> = {
  online: {
    label: "在线",
    tone: "ok",
    transient: false,
    hint: "设备已就绪",
  },
  degraded: {
    label: "降级",
    tone: "warn",
    transient: false,
    hint: "设备在线但部分能力异常",
  },
  offline: {
    label: "离线",
    tone: "neutral",
    transient: false,
    hint: "设备未连接或未启动",
  },
  rebooting: {
    label: "重启中",
    tone: "progress",
    transient: true,
    hint: "模组正在重启",
  },
  usb_wait: {
    label: "等待 USB",
    tone: "progress",
    transient: true,
    hint: "等待 USB 设备重新枚举",
  },
  worker_starting: {
    label: "启动中",
    tone: "progress",
    transient: true,
    hint: "工作线程启动中",
  },
  qmi_starting: {
    label: "QMI 启动中",
    tone: "progress",
    transient: true,
    hint: "QMI 通道建立中",
  },
  recovering: {
    label: "恢复中",
    tone: "progress",
    transient: true,
    hint: "正在从异常中恢复",
  },
  evicting: {
    label: "移除中",
    tone: "danger",
    transient: true,
    hint: "设备正在被移出设备池",
  },
};

/** 未知 phase 也要有确定的展示，不能出现空白。 */
const UNKNOWN_PHASE: PhaseMeta = {
  label: "未知",
  tone: "neutral",
  transient: false,
  hint: "后端返回了未识别的生命周期状态",
};

export function phaseMeta(phase: string | undefined): PhaseMeta {
  if (!phase) return UNKNOWN_PHASE;
  return PHASE_META[phase as LifecyclePhase] ?? UNKNOWN_PHASE;
}

export interface DeviceStatusSummary {
  label: string;
  tone: StatusTone;
  /** 一句话解释当前状态，用于 tooltip 或副标题 */
  detail: string;
  transient: boolean;
}

/**
 * 综合 phase 与布尔位得出展示状态。
 *
 * 判定顺序刻意如此：过渡态优先（用户需要知道"正在做什么"），
 * 其次是明确的故障，最后才细分 online 下的联网/注册情况。
 */
export function summarizeDeviceStatus(d: DeviceOverview): DeviceStatusSummary {
  const meta = phaseMeta(d.lifecycle_phase);

  // 过渡态：直接展示阶段，附带后端给的原因
  if (meta.transient) {
    return {
      label: meta.label,
      tone: meta.tone,
      detail: d.lifecycle_reason || meta.hint,
      transient: true,
    };
  }

  if (d.lifecycle_phase === "offline") {
    return {
      label: meta.label,
      tone: "neutral",
      detail: d.lifecycle_reason || (d.physical_present ? "硬件在位但未启动" : "硬件不在位"),
      transient: false,
    };
  }

  if (d.lifecycle_phase === "degraded" || !d.healthy) {
    return {
      label: "降级",
      tone: "warn",
      detail: d.lifecycle_reason || describeDegradation(d),
      transient: false,
    };
  }

  // online 且健康：进一步区分是否真正可用
  if (!d.radio_registered) {
    return {
      label: "未注册",
      tone: "warn",
      detail: "已在线但未注册到运营商网络",
      transient: false,
    };
  }

  if (d.network_enabled && !d.data_connected) {
    return {
      label: "未联网",
      tone: "warn",
      detail: "已注册网络但数据连接未建立",
      transient: false,
    };
  }

  return {
    label: meta.label,
    tone: meta.tone,
    detail: d.public_ip ? `出口 IP ${d.public_ip}` : meta.hint,
    transient: false,
  };
}

function describeDegradation(d: DeviceOverview): string {
  const problems: string[] = [];
  if (!d.control_online) problems.push("控制通道断开");
  if (!d.worker_running) problems.push("工作线程未运行");
  if (!d.physical_present) problems.push("硬件不在位");
  if (!d.radio_registered) problems.push("未注册网络");
  if (d.network_enabled && !d.data_connected) problems.push("数据未连接");
  return problems.length > 0 ? problems.join("、") : "健康检查未通过";
}

/** 信号强度分级。RSRP 为 0 通常表示无有效读数。 */
export function signalLevel(rsrp: number | undefined): {
  label: string;
  tone: StatusTone;
} {
  if (!rsrp || rsrp === 0) return { label: "无信号", tone: "neutral" };
  if (rsrp >= -85) return { label: "优", tone: "ok" };
  if (rsrp >= -100) return { label: "良", tone: "ok" };
  if (rsrp >= -110) return { label: "中", tone: "warn" };
  return { label: "弱", tone: "danger" };
}

export const TONE_CLASS: Record<StatusTone, string> = {
  ok: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
  warn: "bg-amber-500/15 text-amber-700 dark:text-amber-500",
  danger: "bg-destructive/15 text-destructive",
  progress: "bg-blue-500/15 text-blue-700 dark:text-blue-400",
  neutral: "bg-muted text-muted-foreground",
};
