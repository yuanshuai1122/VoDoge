import { api } from "../client";
import { ApiError } from "../errors";
import { pick, raw, rawArray } from "../unwrap";
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

/** GET /devices/:id/esim/chip-info —— 裸对象 */
export async function getChipInfo(
  deviceId: string,
): Promise<Record<string, unknown>> {
  return raw<Record<string, unknown>>(
    await api.get(`/devices/${encodeURIComponent(deviceId)}/esim/chip-info`, {
      timeoutMs: 60_000,
    }),
  );
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

/** 对齐 internal/esim.NotificationItem */
export interface EsimNotification {
  sequence_number: number;
  event: string;
  iccid?: string;
  address?: string;
  aid_hex?: string;
  can_retry: boolean;
}

/** GET /devices/:id/esim/notifications -> {items} */
export async function listNotifications(
  deviceId: string,
): Promise<EsimNotification[]> {
  const body = await api.get(
    `/devices/${encodeURIComponent(deviceId)}/esim/notifications`,
    { timeoutMs: 60_000 },
  );
  return pick<EsimNotification[]>(body, "items");
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

export interface StartDownloadInput {
  smdp: string;
  matching_id?: string;
  confirmation_code?: string;
  aid_hex?: string;
  imei?: string;
}

/**
 * 发起下载：激活参数走 POST body，只有不敏感的 task_id 会进 URL。
 *
 * 早先是 GET + query，激活码因此会落进浏览器历史、Referer 和中间层访问日志；
 * 激活码通常一次性且可被抢用，那个暴露面不可接受。
 *
 * 设备已有进行中的任务时后端返回 409 + task_id，这里当作正常结果返回，
 * 调用方直接订阅那个任务即可（重复点击、刷新页面都会走到这条路径）。
 */
export async function startDownloadProfile(
  deviceId: string,
  input: StartDownloadInput,
): Promise<{ task_id: string; already_running: boolean }> {
  try {
    const body = await api.post<{ task_id?: string }>(
      `/devices/${encodeURIComponent(deviceId)}/esim/actions/download`,
      input,
      { timeoutMs: 30_000 },
    );
    return { task_id: body?.task_id ?? "", already_running: false };
  } catch (err) {
    if (err instanceof ApiError && err.httpStatus === 409) {
      const taskId = err.body?.task_id;
      if (typeof taskId === "string" && taskId) {
        return { task_id: taskId, already_running: true };
      }
    }
    throw err;
  }
}

/** 订阅下载进度的 SSE 路径；配 ?task_id= 使用。 */
export function downloadProfileStreamPath(deviceId: string): string {
  return `/devices/${encodeURIComponent(deviceId)}/esim/actions/download/stream`;
}
