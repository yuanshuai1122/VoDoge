import { api } from "../client";
import { ok } from "../unwrap";

export interface SystemInfo {
  [key: string]: unknown;
}

export async function getSystemInfo(): Promise<SystemInfo> {
  return api.get<SystemInfo>("/system/info");
}

/** GET /api/traffic/analysis，range 默认 day */
export async function getTrafficAnalysis(params: {
  range?: "day" | "week" | "month";
  device_id?: string;
}): Promise<unknown> {
  return api.get("/traffic/analysis", {
    query: { range: params.range ?? "day", device_id: params.device_id },
  });
}

export async function getNotificationSettings(): Promise<unknown> {
  return api.get("/settings/notifications");
}

export async function updateNotificationSettings(input: unknown): Promise<void> {
  ok(await api.put("/settings/notifications", input));
}

/**
 * 仅 webhook / bark / email 三个渠道有测试接口。
 * Telegram、飞书、QQ、PushPlus **没有**，UI 上不要给这几个渠道提供测试按钮。
 */
export type TestableChannel = "webhook" | "bark" | "email";

export async function testNotification(channel: TestableChannel): Promise<void> {
  ok(await api.post(`/settings/notifications/${channel}/test`, undefined, {
    timeoutMs: 60_000,
  }));
}

export async function checkUpdate(): Promise<unknown> {
  return api.get("/system/update/check", { timeoutMs: 60_000 });
}

export async function applyUpdate(): Promise<void> {
  ok(await api.post("/system/update/apply", undefined, { timeoutMs: 120_000 }));
}

export async function getLogHistory(params?: {
  level?: string;
}): Promise<unknown> {
  return api.get("/logs/history", { query: { level: params?.level } });
}
