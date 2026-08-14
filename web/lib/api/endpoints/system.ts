import { api } from "../client";
import type { LogEntry } from "../../../types/log";

export interface SystemInfo {
  [key: string]: unknown;
}

export async function getSystemInfo(): Promise<SystemInfo> {
  return (await api.get<SystemInfo>("/system/info")).data;
}

/** GET /api/traffic/analysis，range 默认 day */
export async function getTrafficAnalysis(params: {
  range?: "day" | "week" | "month";
  device_id?: string;
}): Promise<unknown> {
  return (
    await api.get("/traffic/analysis", {
      query: { range: params.range ?? "day", device_id: params.device_id },
    })
  ).data;
}

export async function getNotificationSettings(): Promise<unknown> {
  return (await api.get("/settings/notifications")).data;
}

export async function updateNotificationSettings(input: unknown): Promise<void> {
  await api.put("/settings/notifications", input);
}

/**
 * 仅 webhook / bark / email 三个渠道有测试接口。
 * Telegram、飞书、QQ、PushPlus **没有**，UI 上不要给这几个渠道提供测试按钮。
 */
export type TestableChannel = "webhook" | "bark" | "email";

export async function testNotification(channel: TestableChannel): Promise<void> {
  await api.post(`/settings/notifications/${channel}/test`, undefined, {
    timeoutMs: 60_000,
  });
}

export async function checkUpdate(): Promise<unknown> {
  return (await api.get("/system/update/check", { timeoutMs: 60_000 })).data;
}

export async function applyUpdate(): Promise<void> {
  await api.post("/system/update/apply", undefined, { timeoutMs: 120_000 });
}

/**
 * POST /api/system/uninstall —— **不可撤销的自毁**。
 *
 * 后端立刻回 200，然后在 1 秒后的后台协程里依次：
 *  1. 通知 systemd / procd 停止并**禁用自启**（否则删完文件会被拉起来）
 *  2. `os.RemoveAll("data")` —— 数据库以外的全部运行时数据
 *  3. 删除运行时实际加载的配置文件
 *  4. **删除可执行文件自身**，然后 `os.Exit(0)`
 *
 * 因此调用成功不代表"完成"，只代表指令已送达；服务随即消失，
 * 不要在其后再发任何请求或等待轮询结果。
 *
 * PostgreSQL 里的数据**不在**删除范围内——那是外部服务，需要另行处理。
 */
export async function uninstall(): Promise<void> {
  await api.post("/system/uninstall", undefined, { timeoutMs: 30_000 });
}

/**
 * GET /api/logs/history -> {logs}
 * 实测确认是包装形状，不是裸数组。
 */
export async function getLogHistory(params?: {
  level?: string;
}): Promise<LogEntry[]> {
  return (await api.get<LogEntry[]>("/logs/history", { query: { level: params?.level } })).data;
}
