/** 设备线路。对齐 DeviceConfig.Lane：空 | cn | intl，不按 MCC 推断。 */
export type DeviceLane = "" | "cn" | "intl";

export const DEVICE_LANES: { value: DeviceLane; label: string }[] = [
  { value: "", label: "未分线" },
  { value: "cn", label: "国内" },
  { value: "intl", label: "国外" },
];

export function normalizeLane(raw: string | undefined | null): DeviceLane {
  const v = (raw ?? "").trim().toLowerCase();
  if (v === "cn" || v === "intl") return v;
  return "";
}

export function laneLabel(raw: string | undefined | null): string {
  const v = normalizeLane(raw);
  if (v === "cn") return "国内";
  if (v === "intl") return "国外";
  return "";
}
