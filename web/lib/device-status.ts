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
import { t, type MessageKey } from "@/lib/i18n";

export type StatusTone = "ok" | "warn" | "danger" | "neutral" | "progress";

interface PhaseMeta {
  label: string;
  tone: StatusTone;
  /** 是否属于过渡态：过渡态应显示进度感，且不应报警 */
  transient: boolean;
  hint: string;
}

const PHASE_META: Record<
  LifecyclePhase,
  { label: MessageKey; tone: StatusTone; transient: boolean; hint: MessageKey }
> = {
  online: { label: "status.online", tone: "ok", transient: false, hint: "status.hint.online" },
  degraded: { label: "status.degraded", tone: "warn", transient: false, hint: "status.hint.degraded" },
  offline: { label: "status.offline", tone: "neutral", transient: false, hint: "status.hint.offline" },
  rebooting: { label: "status.rebooting", tone: "progress", transient: true, hint: "status.hint.rebooting" },
  usb_wait: { label: "status.usbWait", tone: "progress", transient: true, hint: "status.hint.usbWait" },
  worker_starting: { label: "status.starting", tone: "progress", transient: true, hint: "status.hint.starting" },
  qmi_starting: { label: "status.qmiStarting", tone: "progress", transient: true, hint: "status.hint.qmiStarting" },
  recovering: { label: "status.recovering", tone: "progress", transient: true, hint: "status.hint.recovering" },
  evicting: { label: "status.evicting", tone: "danger", transient: true, hint: "status.hint.evicting" },
};

const UNKNOWN_PHASE = {
  label: "status.unknown" as MessageKey,
  tone: "neutral" as StatusTone,
  transient: false,
  hint: "status.hint.unknown" as MessageKey,
};

export function phaseMeta(phase: string | undefined): PhaseMeta {
  const raw = !phase
    ? UNKNOWN_PHASE
    : (PHASE_META[phase as LifecyclePhase] ?? UNKNOWN_PHASE);
  return {
    label: t(raw.label),
    tone: raw.tone,
    transient: raw.transient,
    hint: t(raw.hint),
  };
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
      detail: d.lifecycle_reason || (d.physical_present ? t("status.hwPresent") : t("status.hwMissing")),
      transient: false,
    };
  }

  if (d.lifecycle_phase === "degraded" || !d.healthy) {
    return {
      label: t("status.degraded"),
      tone: "warn",
      detail: d.lifecycle_reason || describeDegradation(d),
      transient: false,
    };
  }

  // online 且健康：进一步区分是否真正可用
  if (!d.radio_registered) {
    return {
      label: t("status.unregistered"),
      tone: "warn",
      detail: t("status.unregisteredHint"),
      transient: false,
    };
  }

  if (d.network_enabled && !d.data_connected) {
    return {
      label: t("status.noData"),
      tone: "warn",
      detail: t("status.noDataHint"),
      transient: false,
    };
  }

  return {
    label: meta.label,
    tone: meta.tone,
    detail: d.public_ip ? t("status.exitIp", { ip: d.public_ip }) : meta.hint,
    transient: false,
  };
}

function describeDegradation(d: DeviceOverview): string {
  const problems: string[] = [];
  if (!d.control_online) problems.push(t("status.ctrlDown"));
  if (!d.worker_running) problems.push(t("status.workerDown"));
  if (!d.physical_present) problems.push(t("status.hwMissing"));
  if (!d.radio_registered) problems.push(t("status.noRadio"));
  if (d.network_enabled && !d.data_connected) problems.push(t("status.dataDown"));
  return problems.length > 0 ? problems.join("、") : t("status.unhealthy");
}

/** 信号强度分级。RSRP 为 0 通常表示无有效读数。 */
export function signalLevel(rsrp: number | undefined): {
  label: string;
  tone: StatusTone;
} {
  if (!rsrp || rsrp === 0) return { label: t("signal.none"), tone: "neutral" };
  if (rsrp >= -85) return { label: t("signal.excellent"), tone: "ok" };
  if (rsrp >= -100) return { label: t("signal.good"), tone: "ok" };
  if (rsrp >= -110) return { label: t("signal.fair"), tone: "warn" };
  return { label: t("signal.weak"), tone: "danger" };
}

export const TONE_CLASS: Record<StatusTone, string> = {
  ok: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
  warn: "bg-amber-500/15 text-amber-700 dark:text-amber-500",
  danger: "bg-destructive/15 text-destructive",
  progress: "bg-blue-500/15 text-blue-700 dark:text-blue-400",
  neutral: "bg-muted text-muted-foreground",
};
