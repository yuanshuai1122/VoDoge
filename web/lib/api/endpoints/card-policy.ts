import { api } from "../client";

/** 对齐 internal/db.CardPolicy。策略跟 ICCID 走，不跟设备。 */
export interface CardPolicy {
  iccid: string;
  network_enabled: boolean;
  vowifi_enabled: boolean;
  airplane_enabled: boolean;
  ip_version: string;
  apn: string;
  /** auto = 系统推导，user = 用户显式设置 */
  source: string;
  created_at?: string;
  updated_at?: string;
}

/**
 * GET /api/cards/:iccid/policy —— 裸对象。
 * 策略不存在时后端返回 DefaultCardPolicy 而非 404，因此这里不会抛 not found。
 */
export async function getCardPolicy(iccid: string): Promise<CardPolicy> {
  return (await api.get<CardPolicy>(`/cards/${encodeURIComponent(iccid)}/policy`)).data;
}

/** PUT /api/cards/:iccid/policy —— 返回更新后的裸对象 */
export async function putCardPolicy(
  iccid: string,
  input: Partial<CardPolicy>,
): Promise<CardPolicy> {
  return (await api.put<CardPolicy>(`/cards/${encodeURIComponent(iccid)}/policy`, input)).data;
}

/** GET /api/cards/policies -> {policies} */
export async function listCardPolicies(): Promise<CardPolicy[]> {
  return (await api.get<CardPolicy[]>("/cards/policies")).data;
}
