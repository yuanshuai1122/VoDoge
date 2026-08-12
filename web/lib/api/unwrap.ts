/**
 * 成功响应归一化。
 *
 * 后端 108 处 200 响应中仅 47 处用 {status:"ok"} 包装，其余散落在至少 6 种形状
 * （见 docs/frontend-api-matrix.md §2.1）：
 *
 *   {status:"ok", message}          {status:"ok", response:X}
 *   {devices:[...]}                 {devices:[...], device_limit:N}
 *   {config:X} {items:[...]} {policies:[...]}
 *   裸数组 / 裸对象
 *
 * 设计原则：**每个 endpoint 显式声明自己期望的形状**，不做运行时猜测。
 * 猜测会在字段名撞车时静默返回错误的数据，比抛错更难排查。
 */

import { ApiError } from "./errors";

function asRecord(v: unknown): Record<string, unknown> | null {
  return typeof v === "object" && v !== null && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : null;
}

/**
 * 取包装键的值，兼容有无 {status:"ok"} 外壳。
 *
 *   pick({devices:[...]}, "devices")               -> [...]
 *   pick({status:"ok", devices:[...]}, "devices")  -> [...]
 */
export function pick<T>(body: unknown, key: string): T {
  const rec = asRecord(body);
  if (!rec || !(key in rec)) {
    throw new ApiError(`响应缺少字段 "${key}"`, { httpStatus: 200 });
  }
  return rec[key] as T;
}

/**
 * 同 pick，但字段缺失时返回兜底值而非抛错。
 * 用于后端可能省略的可选集合（omitempty 字段）。
 */
export function pickOr<T>(body: unknown, key: string, fallback: T): T {
  const rec = asRecord(body);
  if (!rec || !(key in rec) || rec[key] == null) return fallback;
  return rec[key] as T;
}

/** 裸数组 / 裸对象响应（如 /sms/contacts、/cards/:iccid/policy）。 */
export function raw<T>(body: unknown): T {
  return body as T;
}

/** 裸数组响应；null/非数组一律归一为 []，避免 .map 崩溃。 */
export function rawArray<T>(body: unknown): T[] {
  return Array.isArray(body) ? (body as T[]) : [];
}

/**
 * 仅需确认操作成功、无有效载荷的端点。
 * 后端此类响应形如 {status:"ok", message:"..."}。
 */
export function ok(body: unknown): void {
  const rec = asRecord(body);
  if (rec && rec.status === "error") {
    throw new ApiError(
      typeof rec.message === "string" ? rec.message : "操作失败",
      { httpStatus: 200 },
    );
  }
}

/**
 * 设备详情专用：GET /devices/:id/overview 返回的是 {devices:[单元素]}，
 * 不是单对象。忘记取 [0] 是个很容易犯的错，这里固化掉。
 */
export function pickFirstDevice<T>(body: unknown): T {
  const list = pick<T[]>(body, "devices");
  if (!Array.isArray(list) || list.length === 0) {
    throw new ApiError("设备不存在或已离线", { httpStatus: 404 });
  }
  return list[0];
}
