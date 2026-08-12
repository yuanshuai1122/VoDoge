import { api } from "../client";
import { ok, rawArray } from "../unwrap";
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
  const body = await api.get("/sms/contacts", {
    query: {
      device_id: params.device_id,
      imsi: params.imsi,
      limit: params.limit ?? SMS_PAGE_SIZE,
      before_ts: params.cursor?.before_ts,
      before_peer: params.cursor?.before_peer,
    },
  });
  return rawArray<SMSContact>(body);
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
  const body = await api.get("/sms/thread", {
    query: {
      peer: params.peer,
      device_id: params.device_id,
      imsi: params.imsi,
      limit: params.limit ?? SMS_PAGE_SIZE,
      before_ts: params.cursor?.before_ts,
      before_id: params.cursor?.before_id,
    },
  });
  return rawArray<SMSMessage>(body);
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

export async function sendSMS(input: {
  phone: string;
  message: string;
  device_id?: string;
  imsi?: string;
  encoding?: string;
}): Promise<void> {
  ok(await api.post("/sms/send", input, { timeoutMs: 60_000 }));
}

export async function getDeliveryStatus(messageId: string): Promise<unknown> {
  return api.get(`/sms/delivery/${encodeURIComponent(messageId)}`);
}

export async function deleteMessage(id: number): Promise<void> {
  ok(await api.delete(`/sms/messages/${id}`));
}

/** DELETE /api/sms/thread —— 通过 query 指定会话 */
export async function deleteThread(params: {
  peer: string;
  device_id?: string;
  imsi?: string;
}): Promise<void> {
  ok(
    await api.delete("/sms/thread", {
      query: {
        peer: params.peer,
        device_id: params.device_id,
        imsi: params.imsi,
      },
    }),
  );
}
