import { api } from "../client";
import type { LogEntry } from "../../../types/log";
import type { SMSSettings } from "../../../types/sms";

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

/** GET /api/settings/sms */
export async function getSMSSettings(): Promise<SMSSettings> {
  return (await api.get<SMSSettings>("/settings/sms")).data;
}

/** PUT /api/settings/sms */
export async function updateSMSSettings(input: {
  hourly_limit: number;
}): Promise<SMSSettings> {
  return (await api.put<SMSSettings>("/settings/sms", input)).data;
}

export interface DeviceQuota {
  limit: number;
  used: number;
  default_limit: number;
  max_limit: number;
}

/** GET /api/settings/devices */
export async function getDeviceQuota(): Promise<DeviceQuota> {
  return (await api.get<DeviceQuota>("/settings/devices")).data;
}

/** PUT /api/settings/devices */
export async function updateDeviceQuota(input: {
  limit: number;
}): Promise<DeviceQuota> {
  return (await api.put<DeviceQuota>("/settings/devices", input)).data;
}

export interface HTTPSSettings {
  enabled: boolean;
  http_url: string;
  https_url: string;
  fingerprint?: string;
  not_after?: string;
}

/** GET /api/settings/https */
export async function getHTTPSSettings(): Promise<HTTPSSettings> {
  return (await api.get<HTTPSSettings>("/settings/https")).data;
}

/** PUT /api/settings/https */
export async function updateHTTPSSettings(input: {
  enabled: boolean;
}): Promise<HTTPSSettings> {
  return (await api.put<HTTPSSettings>("/settings/https", input)).data;
}

export interface SecuritySettings {
  mode: "internal" | "public" | string;
  allowed_cidrs: string[];
  trust_proxy_headers: boolean;
  client_ip: string;
  client_allowed: boolean;
}

/** GET /api/settings/security */
export async function getSecuritySettings(): Promise<SecuritySettings> {
  return (await api.get<SecuritySettings>("/settings/security")).data;
}

/** PUT /api/settings/security */
export async function updateSecuritySettings(input: {
  mode: string;
  allowed_cidrs: string[];
  trust_proxy_headers: boolean;
}): Promise<SecuritySettings> {
  return (await api.put<SecuritySettings>("/settings/security", input)).data;
}

/** GET /api/settings/https/certificate —— PEM，不走 JSON 信封 */
export async function downloadHTTPSCertificate(): Promise<void> {
  const { getToken } = await import("../../auth/token");
  const { parseApiError } = await import("../errors");
  const token = getToken();
  const headers = new Headers();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch("/api/settings/https/certificate", { headers });
  if (!res.ok) {
    let body: unknown;
    try {
      body = await res.json();
    } catch {
      body = undefined;
    }
    throw parseApiError(res.status, body);
  }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "vodog-selfsigned.crt";
  a.click();
  URL.revokeObjectURL(url);
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
