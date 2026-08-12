import { api } from "../client";
import { rawArray } from "../unwrap";
import type { EUICCProfiles } from "../../../types/esim";

/**
 * eSIM 端点。
 *
 * 两点与其它域不同：
 *  - 错误体是 {error:"..."}，不是 {status:"error", message}（已由 errors.ts 归一）
 *  - 所有操作经 APDU 仲裁器串行化，任一调用都可能返回 409 ESIM_BUSY，
 *    调用方必须配合 stores/esim-busy 做互斥，不要盲目重试
 */

/** GET /devices/:id/esim/profiles —— 裸数组，按 eUICC 分组 */
export async function listProfiles(deviceId: string): Promise<EUICCProfiles[]> {
  const body = await api.get(
    `/devices/${encodeURIComponent(deviceId)}/esim/profiles`,
    { timeoutMs: 60_000 },
  );
  return rawArray<EUICCProfiles>(body);
}

export async function getEsimOverview(deviceId: string): Promise<unknown> {
  return api.get(`/devices/${encodeURIComponent(deviceId)}/esim`, {
    timeoutMs: 60_000,
  });
}

export async function getChipInfo(deviceId: string): Promise<unknown> {
  return api.get(`/devices/${encodeURIComponent(deviceId)}/esim/chip-info`, {
    timeoutMs: 60_000,
  });
}

export async function switchProfile(
  deviceId: string,
  input: { iccid: string; aid_hex?: string },
): Promise<void> {
  // 后端固定 30s 超时并等待生效，客户端需留出余量
  await api.post(
    `/devices/${encodeURIComponent(deviceId)}/esim/actions/switch`,
    input,
    { timeoutMs: 60_000 },
  );
}

export async function renameProfile(
  deviceId: string,
  iccid: string,
  input: { name: string; aid_hex?: string },
): Promise<void> {
  await api.patch(
    `/devices/${encodeURIComponent(deviceId)}/esim/profiles/${encodeURIComponent(iccid)}`,
    input,
    { timeoutMs: 60_000 },
  );
}

/** 删除成功可能带 warning / warning_code / space_delta，需展示而非忽略。 */
export interface DeleteProfileResult {
  status?: string;
  message?: string;
  warning?: string;
  warning_code?: string;
  space_delta?: unknown;
}

export async function deleteProfile(
  deviceId: string,
  iccid: string,
): Promise<DeleteProfileResult> {
  return api.delete<DeleteProfileResult>(
    `/devices/${encodeURIComponent(deviceId)}/esim/profiles/${encodeURIComponent(iccid)}`,
    { timeoutMs: 120_000 },
  );
}

export async function listNotifications(deviceId: string): Promise<unknown> {
  return api.get(`/devices/${encodeURIComponent(deviceId)}/esim/notifications`, {
    timeoutMs: 60_000,
  });
}

export async function retryNotification(
  deviceId: string,
  sequence: number,
): Promise<void> {
  await api.post(
    `/devices/${encodeURIComponent(deviceId)}/esim/notifications/${sequence}/actions/retry`,
    undefined,
    { timeoutMs: 60_000 },
  );
}

/**
 * 下载 profile 的 SSE 路径与查询参数。
 *
 * 注意这是 GET + query，激活码（confirmation_code）会进入浏览器历史；
 * 后端访问日志已对 token 脱敏，但激活码本身没有脱敏（api-matrix §8-4）。
 */
export function downloadProfileStreamPath(deviceId: string): string {
  return `/devices/${encodeURIComponent(deviceId)}/esim/actions/download`;
}
