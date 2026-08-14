import { api } from "../client";
import { ApiError } from "../errors";
import type { EUICCProfiles } from "../../../types/esim";

/**
 * eSIM 端点。
 *
 * 所有操作经 APDU 仲裁器串行化，任一调用都可能返回 409 ESIM_BUSY，
 * 调用方必须配合 stores/esim-lock 做互斥，不要盲目重试。
 */

/** GET /devices/:id/esim/profiles —— 按 eUICC 分组的数组 */
export async function listProfiles(deviceId: string): Promise<EUICCProfiles[]> {
  return (
    await api.get<EUICCProfiles[]>(
      `/devices/${encodeURIComponent(deviceId)}/esim/profiles`,
      { timeoutMs: 60_000 },
    )
  ).data;
}

export async function getEsimOverview(deviceId: string): Promise<unknown> {
  return (
    await api.get(`/devices/${encodeURIComponent(deviceId)}/esim`, {
      timeoutMs: 60_000,
    })
  ).data;
}

export async function getChipInfo(
  deviceId: string,
): Promise<Record<string, unknown>> {
  return (
    await api.get<Record<string, unknown>>(
      `/devices/${encodeURIComponent(deviceId)}/esim/chip-info`,
      { timeoutMs: 60_000 },
    )
  ).data;
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

/**
 * 删除的结果全在 meta：提示语、通知未确认的告警、eUICC 空间变化。
 * 被删掉的 profile 没有资源可返回，data 为 null。
 * warning 必须展示而不是忽略——它表示删成功了但通知没送达运营商。
 */
export interface DeleteProfileResult {
  message?: string;
  warning?: string;
  warning_code?: string;
  space_delta?: unknown;
}

export async function deleteProfile(
  deviceId: string,
  iccid: string,
): Promise<DeleteProfileResult> {
  const { meta } = await api.delete(
    `/devices/${encodeURIComponent(deviceId)}/esim/profiles/${encodeURIComponent(iccid)}`,
    { timeoutMs: 120_000 },
  );
  return {
    message: typeof meta.message === "string" ? meta.message : undefined,
    warning: typeof meta.warning === "string" ? meta.warning : undefined,
    warning_code:
      typeof meta.warning_code === "string" ? meta.warning_code : undefined,
    space_delta: meta.space_delta,
  };
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

export async function listNotifications(
  deviceId: string,
): Promise<EsimNotification[]> {
  return (
    await api.get<EsimNotification[]>(
      `/devices/${encodeURIComponent(deviceId)}/esim/notifications`,
      { timeoutMs: 60_000 },
    )
  ).data;
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
    const { data } = await api.post<{ task_id?: string }>(
      `/devices/${encodeURIComponent(deviceId)}/esim/actions/download`,
      input,
      { timeoutMs: 30_000 },
    );
    return { task_id: data?.task_id ?? "", already_running: false };
  } catch (err) {
    if (err instanceof ApiError && err.httpStatus === 409) {
      // 已有任务时 task_id 在 error.details 里——那是客户端要据以决策的数据
      const taskId = err.details?.task_id;
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
