/** 设备线路。对齐 DeviceConfig.Lane：空 | cn | intl，不按 MCC 推断。 */
import { t, type MessageKey } from "@/lib/i18n";

export type DeviceLane = "" | "cn" | "intl";

export const DEVICE_LANES: { value: DeviceLane; labelKey: MessageKey }[] = [
  { value: "", labelKey: "lane.none" },
  { value: "cn", labelKey: "lane.cn" },
  { value: "intl", labelKey: "lane.intl" },
];

export function laneOptions(): { value: DeviceLane; label: string }[] {
  return DEVICE_LANES.map((opt) => ({ value: opt.value, label: t(opt.labelKey) }));
}

export function normalizeLane(raw: string | undefined | null): DeviceLane {
  const v = (raw ?? "").trim().toLowerCase();
  if (v === "cn" || v === "intl") return v;
  return "";
}

export function laneLabel(raw: string | undefined | null): string {
  const v = normalizeLane(raw);
  if (v === "cn") return t("lane.cn");
  if (v === "intl") return t("lane.intl");
  return "";
}
