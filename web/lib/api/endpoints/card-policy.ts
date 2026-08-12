import { api } from "../client";
import { pick, raw } from "../unwrap";

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
  return raw<CardPolicy>(
    await api.get(`/cards/${encodeURIComponent(iccid)}/policy`),
  );
}

/** PUT /api/cards/:iccid/policy —— 返回更新后的裸对象 */
export async function putCardPolicy(
  iccid: string,
  input: Partial<CardPolicy>,
): Promise<CardPolicy> {
  return raw<CardPolicy>(
    await api.put(`/cards/${encodeURIComponent(iccid)}/policy`, input),
  );
}

/** GET /api/cards/policies -> {policies} */
export async function listCardPolicies(): Promise<CardPolicy[]> {
  return pick<CardPolicy[]>(await api.get("/cards/policies"), "policies");
}
