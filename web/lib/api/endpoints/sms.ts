import { api } from "../client";
import type { SMSContact, SMSMessage } from "../../../types/sms";

/**
 * 短信端点。
 *
 * 两个与其它域不同的地方：
 *  - contacts / thread 返回**裸数组**，不是 {status:"ok"} 包装
 *  - 分页是**游标式**，没有总数也没有 has_more；判断「还有更多」只能靠
 *    「返回条数 === limit」，因此适合 useInfiniteQuery 而非页码分页
 */

export const SMS_PAGE_SIZE = 50;

export interface ContactsCursor {
  before_ts?: string;
  before_peer?: string;
}

/** GET /api/sms/contacts —— 裸数组 */
export async function listContacts(params: {
  device_id?: string;
  imsi?: string;
  limit?: number;
  cursor?: ContactsCursor;
}): Promise<SMSContact[]> {
  const { data } = await api.get<SMSContact[]>("/sms/contacts", {
    query: {
      device_id: params.device_id,
      imsi: params.imsi,
      limit: params.limit ?? SMS_PAGE_SIZE,
      before_ts: params.cursor?.before_ts,
      before_peer: params.cursor?.before_peer,
    },
  });
  return data;
}

export interface ThreadCursor {
  before_ts?: string;
  before_id?: number;
}

/** GET /api/sms/thread —— 裸数组，peer 必填（缺失返回 400） */
export async function listThread(params: {
  peer: string;
  device_id?: string;
  imsi?: string;
  limit?: number;
  cursor?: ThreadCursor;
}): Promise<SMSMessage[]> {
  const { data } = await api.get<SMSMessage[]>("/sms/thread", {
    query: {
      peer: params.peer,
      device_id: params.device_id,
      imsi: params.imsi,
      limit: params.limit ?? SMS_PAGE_SIZE,
      before_ts: params.cursor?.before_ts,
      before_id: params.cursor?.before_id,
    },
  });
  return data;
}

/** 从当前页末元素推导下一页游标；不足一页表示已到底。 */
export function nextContactsCursor(
  page: SMSContact[],
  limit = SMS_PAGE_SIZE,
): ContactsCursor | undefined {
  if (page.length < limit) return undefined;
  const last = page[page.length - 1];
  return { before_ts: last.last_timestamp, before_peer: last.peer };
}

export function nextThreadCursor(
  page: SMSMessage[],
  limit = SMS_PAGE_SIZE,
): ThreadCursor | undefined {
  if (page.length < limit) return undefined;
  const last = page[page.length - 1];
  return { before_ts: last.timestamp, before_id: last.id };
}

export interface SendSMSResult {
  /** 操作说明，来自 meta */
  message: string;
  device: string;
  phone: string;
  /** 仅 VoWiFi 通道会生成；AT 通道为空 */
  message_id: string;
  /** 长短信被拆成的分片数 */
  parts_total: number;
  delivery_state: string;
}

export async function sendSMS(input: {
  phone: string;
  message: string;
  device_id?: string;
  imsi?: string;
  encoding?: string;
}): Promise<SendSMSResult> {
  const { data, meta } = await api.post<Omit<SendSMSResult, "message">>(
    "/sms/send",
    input,
    { timeoutMs: 60_000 },
  );
  return {
    ...data,
    message: typeof meta.message === "string" ? meta.message : "",
  };
}

/** 对齐 internal/db.SMSDelivery */
export interface SMSDelivery {
  message_id: string;
  imsi: string;
  iccid: string;
  device_id: string;
  peer: string;
  content: string;
  /** 长短信分片总数 */
  parts_total: number;
  /** 已收到确认的分片数 */
  acks: number;
  state: string;
  last_error: string;
  created_at?: string;
  updated_at?: string;
}

/**
 * GET /api/sms/delivery/:message_id -> {status:"ok", delivery}
 *
 * 只有 VoWiFi 通道发出的短信才有投递记录（后端遍历 VoWiFi app 查询），
 * AT 通道发送不会产生 message_id，因此查不到属正常，会返回 404。
 */
export async function getDeliveryStatus(
  messageId: string,
): Promise<SMSDelivery> {
  return (await api.get<SMSDelivery>(`/sms/delivery/${encodeURIComponent(messageId)}`)).data;
}

export async function deleteMessage(id: number): Promise<void> {
  await api.delete(`/sms/messages/${id}`);
}

/** DELETE /api/sms/thread —— 通过 query 指定会话 */
export async function deleteThread(params: {
  peer: string;
  device_id?: string;
  imsi?: string;
}): Promise<void> {
  await api.delete("/sms/thread", {
      query: {
        peer: params.peer,
        device_id: params.device_id,
        imsi: params.imsi,
      },
    });
}
