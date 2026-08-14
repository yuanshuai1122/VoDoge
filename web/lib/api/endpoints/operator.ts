import { api } from "../client";

/** 对齐 internal/backend.OperatorSelectionMode */
export type OperatorSelectionMode = "automatic" | "manual";

/** 对齐 internal/backend.OperatorCandidate */
export interface OperatorCandidate {
  plmn: string;
  mcc: string;
  mnc: string;
  mnc_length: number;
  includes_pcs_digit: boolean;
  operator_name: string;
  short_name?: string;
  /** available / current / forbidden 等，由模组返回 */
  status: string;
  rats?: string[];
}

/** 对齐 internal/backend.OperatorSelection */
export interface OperatorSelection {
  mode: OperatorSelectionMode;
  plmn?: string;
  mcc?: string;
  mnc?: string;
  mnc_length?: number;
  includes_pcs_digit?: boolean;
  rat?: string;
  operator_name?: string;
}

/** 对齐 internal/api.operatorScanResponse，也是扫描 SSE 的帧格式 */
export interface OperatorScanResponse {
  scan_id: string;
  status: string;
  started_at: string;
  updated_at: string;
  complete: boolean;
  retryable: boolean;
  message: string;
  error?: string;
  candidates: OperatorCandidate[];
}

/** GET /devices/:id/operator_selection —— 裸对象 */
export async function getOperatorSelection(
  deviceId: string,
): Promise<OperatorSelection> {
  return (await api.get<OperatorSelection>(`/devices/${encodeURIComponent(deviceId)}/operator_selection`)).data;
}

/** POST /devices/:id/operator_selection —— 返回更新后的选择 */
export async function setOperatorSelection(
  deviceId: string,
  input: {
    mode: OperatorSelectionMode;
    plmn?: string;
    mcc?: string;
    mnc?: string;
    mnc_length?: number;
    includes_pcs_digit?: boolean;
    rat?: string;
  },
): Promise<OperatorSelection> {
  return (await api.post<OperatorSelection>(
      `/devices/${encodeURIComponent(deviceId)}/operator_selection`,
      input,
      // 锁网需要重新搜网，耗时较长
      { timeoutMs: 180_000 },
    )).data;
}

/** SSE 扫描流路径。扫描可能耗时数十秒，用流式获取中间结果。 */
export function operatorScanStreamPath(deviceId: string): string {
  return `/devices/${encodeURIComponent(deviceId)}/operator_selection/scan/stream`;
}
