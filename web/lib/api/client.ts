/**
 * HTTP 客户端。
 *
 * 生产形态下前端由 Go 嵌入托管，与 API 同源，base 为空即可（相对 /api/...）。
 * 开发期 next dev 通过 next.config.ts 的 rewrites 把 /api/* 反代到 :7575，
 * 同样是同源——后端**没有全局 CORS 中间件**，跨域直连只有 /api/logs/stream 能用，
 * 因此不要把 NEXT_PUBLIC_API_BASE 指向另一个 origin，除非自行解决 CORS。
 */

import { parseApiError, ApiError } from "./errors";
import { getToken, triggerLogout } from "../auth/token";

export const API_BASE = process.env.NEXT_PUBLIC_API_BASE ?? "";

/**
 * 一次成功请求的结果。
 *
 * data 是资源本身，meta 是关于这次操作的说明（告警、是否需重启、额度上限…）。
 * 后端把两者分了层，前端就不必再猜哪个字段是数据、哪个是对数据的注解。
 */
export interface ApiResult<T> {
  data: T;
  meta: Record<string, unknown>;
  /** 与服务端访问日志对应，排查时用它定位。 */
  requestId: string;
}

/** 默认请求超时。eSIM 等长耗时操作应显式放大。 */
const DEFAULT_TIMEOUT_MS = 30_000;

export interface RequestOptions extends Omit<RequestInit, "body"> {
  /** 自动 JSON 序列化并设置 Content-Type。 */
  json?: unknown;
  /** 查询参数，值为 undefined/null/"" 的键会被跳过。 */
  query?: Record<string, string | number | boolean | undefined | null>;
  timeoutMs?: number;
  /** 401 时不触发全局登出（登录页自身请求需要）。 */
  skipAuthRedirect?: boolean;
}

export function buildUrl(
  path: string,
  query?: RequestOptions["query"],
): string {
  const base = `${API_BASE}/api${path.startsWith("/") ? path : `/${path}`}`;
  if (!query) return base;

  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === "") continue;
    sp.append(k, String(v));
  }
  const qs = sp.toString();
  return qs ? `${base}?${qs}` : base;
}

export async function apiFetch<T = unknown>(
  path: string,
  options: RequestOptions = {},
): Promise<ApiResult<T>> {
  const {
    json,
    query,
    timeoutMs = DEFAULT_TIMEOUT_MS,
    skipAuthRedirect = false,
    headers,
    signal,
    ...rest
  } = options;

  const url = buildUrl(path, query);
  const finalHeaders = new Headers(headers);

  const token = getToken();
  if (token) finalHeaders.set("Authorization", `Bearer ${token}`);

  let body: BodyInit | undefined;
  if (json !== undefined) {
    finalHeaders.set("Content-Type", "application/json");
    body = JSON.stringify(json);
  }

  // 组合调用方的 signal 与超时
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  if (signal) {
    if (signal.aborted) controller.abort();
    else signal.addEventListener("abort", () => controller.abort(), { once: true });
  }

  let res: Response;
  try {
    res = await fetch(url, {
      ...rest,
      headers: finalHeaders,
      body,
      signal: controller.signal,
    });
  } catch (e) {
    clearTimeout(timer);
    // 调用方主动取消不应变成错误提示
    if (signal?.aborted) throw e;
    const aborted = e instanceof DOMException && e.name === "AbortError";
    throw new ApiError(aborted ? "请求超时" : "网络连接失败", { httpStatus: 0 });
  }
  clearTimeout(timer);

  const payload = await readBody(res);

  if (!res.ok) {
    const err = parseApiError(res.status, payload);
    if (err.isUnauthorized && !skipAuthRedirect) {
      // token 失效的最常见来源是改密（HMAC 密钥即密码），也可能是 30 天到期
      triggerLogout();
    }
    throw err;
  }

  return unwrapEnvelope<T>(payload);
}

/**
 * 拆开成功信封。
 *
 * 后端所有 2xx 响应都是 {data, meta?, request_id}。这里拆成 ApiResult 之后，
 * 端点层要么取 .data，要么取 .meta——不再需要"每个 endpoint 显式声明自己
 * 期望哪种形状"那套约定，因为形状只有一种了。
 *
 * /ping 是唯一的例外（它在 /api 之外，给外部监控用），不走这条路径。
 */
function unwrapEnvelope<T>(payload: unknown): ApiResult<T> {
  if (typeof payload !== "object" || payload === null || Array.isArray(payload)) {
    // 非对象响应只可能来自不套信封的少数路径（如纯文本日志），原样透传
    return { data: payload as T, meta: {}, requestId: "" };
  }
  const rec = payload as Record<string, unknown>;
  if (!("data" in rec)) {
    // 2xx 却没有 data：要么是漏改的端点，要么是代理插了一脚。
    // 静默当成载荷会让问题一路飘到渲染层，不如在此处说清楚。
    throw new ApiError("响应不符合信封结构（缺少 data 字段）", {
      httpStatus: 200,
      body: rec,
    });
  }
  const meta =
    typeof rec.meta === "object" && rec.meta !== null && !Array.isArray(rec.meta)
      ? (rec.meta as Record<string, unknown>)
      : {};
  return {
    data: rec.data as T,
    meta,
    requestId: typeof rec.request_id === "string" ? rec.request_id : "",
  };
}

async function readBody(res: Response): Promise<unknown> {
  if (res.status === 204) return null;

  const text = await res.text();
  if (text === "") return null;

  const type = res.headers.get("Content-Type") ?? "";
  if (!type.includes("json")) return text;

  try {
    return JSON.parse(text);
  } catch {
    // 后端个别路径在异常时会返回纯文本
    return text;
  }
}

export const api = {
  get: <T>(path: string, options?: RequestOptions) =>
    apiFetch<T>(path, { ...options, method: "GET" }),
  post: <T>(path: string, json?: unknown, options?: RequestOptions) =>
    apiFetch<T>(path, { ...options, method: "POST", json }),
  put: <T>(path: string, json?: unknown, options?: RequestOptions) =>
    apiFetch<T>(path, { ...options, method: "PUT", json }),
  patch: <T>(path: string, json?: unknown, options?: RequestOptions) =>
    apiFetch<T>(path, { ...options, method: "PATCH", json }),
  delete: <T>(path: string, options?: RequestOptions) =>
    apiFetch<T>(path, { ...options, method: "DELETE" }),
};
