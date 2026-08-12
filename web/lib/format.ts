/** 展示层格式化工具。后端时间统一以 UTC 存储，返回 RFC3339。 */

export function formatDateTime(value: string | Date | undefined | null): string {
  if (!value) return "-";
  const d = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleString("zh-CN", { hour12: false });
}

export function formatRelativeTime(
  value: string | Date | undefined | null,
): string {
  if (!value) return "";
  const d = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(d.getTime())) return "";

  const diffMs = Date.now() - d.getTime();
  const diffMin = Math.floor(diffMs / 60_000);

  if (diffMin < 1) return "刚刚";
  if (diffMin < 60) return `${diffMin} 分钟前`;

  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour} 小时前`;

  const diffDay = Math.floor(diffHour / 24);
  if (diffDay < 7) return `${diffDay} 天前`;

  return d.toLocaleDateString("zh-CN");
}

export function formatBytes(bytes: number | undefined | null): string {
  if (bytes == null || Number.isNaN(bytes)) return "-";
  if (bytes < 1024) return `${bytes} B`;

  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 100 ? 0 : 1)} ${units[unit]}`;
}

/**
 * 敏感标识默认打码（IMEI / ICCID / IMSI）。
 * 保留首尾便于人工核对，中间隐藏。
 */
export function maskIdentifier(
  value: string | undefined | null,
  visible = 4,
): string {
  if (!value) return "-";
  if (value.length <= visible * 2) return value;
  return `${value.slice(0, visible)}${"*".repeat(Math.min(8, value.length - visible * 2))}${value.slice(-visible)}`;
}
