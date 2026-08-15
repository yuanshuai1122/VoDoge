/** eSIM DTO。对齐 internal/esim/manager.go。 */

/** ProfileItem.state */
export const PROFILE_STATE_DISABLED = 0;
export const PROFILE_STATE_ENABLED = 1;

export interface ProfileItem {
  iccid: string;
  name: string;
  service_provider_name: string;
  /** 0=disabled, 1=enabled */
  state: number;
  state_text: string;
  class_text?: string;
}

/** GET /devices/:id/esim/profiles 返回按 eUICC 分组的裸数组。 */
export interface EUICCProfiles {
  eid: string;
  aid_hex: string;
  profiles: ProfileItem[];
}

/** eSIM 下载 SSE 的帧格式（该流没有 event 名，走默认 message）。 */
export interface EsimDownloadEvent {
  /** 进度步骤；完成为 "done"，失败为 "error" */
  step: string;
  msg: string;
  /** 进度百分比；失败时为 -1 */
  pct: number;
  /** 仅 step="error" */
  code?: string;
  details?: string;
  /** 仅 step="done" */
  space_delta?: unknown;
  warning?: string;
  warning_code?: string;
}

export function isProfileEnabled(p: ProfileItem): boolean {
  return p.state === PROFILE_STATE_ENABLED;
}

export function currentProfile(
  groups: EUICCProfiles[] | undefined,
): { profile: ProfileItem; aid_hex: string; eid: string } | null {
  if (!groups) return null;
  for (const group of groups) {
    for (const profile of group.profiles) {
      if (isProfileEnabled(profile)) {
        return { profile, aid_hex: group.aid_hex, eid: group.eid };
      }
    }
  }
  return null;
}
