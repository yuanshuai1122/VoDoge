/**
 * 错误响应归一化。
 *
 * 后端只有一种错误形状：
 *
 *   {"error": {"code", "message", "details"?}, "request_id": "..."}
 *
 * `data` 与 `error` 互斥且必有其一，所以判别是结构性的——不再靠
 * `status:"ok"` 这种魔法字符串。那个字符串曾经出现在 200 响应里表示失败，
 * 自相矛盾且无法防。
 *
 * `code` 多数时候只是按 HTTP 状态推导的通用码（`not_found`/`conflict`…），
 * 业务层优先用 httpStatus 分支；只有 `ESIM_BUSY`、`ESIM_DOWNLOAD_IN_PROGRESS`、
 * `e911_*`、`websheet_*` 这类专属码值得直接判。
 *
 * 需要据以决策的结构化数据在 `error.details` 里，与给人读的 message 分开。
 */

export class ApiError extends Error {
  /** HTTP 状态码。0 表示网络层失败（断网、超时、CORS）。 */
  readonly httpStatus: number;
  readonly code?: string;
  readonly requestId?: string;
  /** eSIM 操作因 APDU 仲裁被占用。 */
  readonly busy: boolean;
  /** 仅 busy 时有值，建议的重试等待毫秒数。 */
  readonly retryAfterMs?: number;
  /** 仅 busy 时有值，占用原因（如 "rename_profile"）。 */
  readonly reason?: string;
  /**
   * error.details 原样。少数端点在这里放调用方需要的数据——例如 eSIM 下载
   * 的 409 会带上进行中任务的 task_id，调用方要用它去订阅进度。
   */
  readonly details?: Record<string, unknown>;
  /** 原始响应体，排查用。 */
  readonly body?: Record<string, unknown>;

  constructor(
    message: string,
    opts: {
      httpStatus: number;
      code?: string;
      requestId?: string;
      busy?: boolean;
      retryAfterMs?: number;
      reason?: string;
      details?: Record<string, unknown>;
      body?: Record<string, unknown>;
    },
  ) {
    super(message);
    this.name = "ApiError";
    this.httpStatus = opts.httpStatus;
    this.code = opts.code;
    this.requestId = opts.requestId;
    this.busy = opts.busy ?? false;
    this.retryAfterMs = opts.retryAfterMs;
    this.reason = opts.reason;
    this.details = opts.details;
    this.body = opts.body;
  }

  get isUnauthorized(): boolean {
    return this.httpStatus === 401;
  }

  get isRateLimited(): boolean {
    return this.httpStatus === 429;
  }

  /** 网络层失败，通常可重试。 */
  get isNetworkError(): boolean {
    return this.httpStatus === 0;
  }
}

function asRecord(v: unknown): Record<string, unknown> | null {
  return typeof v === "object" && v !== null && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : null;
}

function asString(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() !== "" ? v : undefined;
}

function asNumber(v: unknown): number | undefined {
  return typeof v === "number" && Number.isFinite(v) ? v : undefined;
}

/** 把错误响应体归一为 ApiError。 */
export function parseApiError(httpStatus: number, body: unknown): ApiError {
  const rec = asRecord(body);
  if (!rec) {
    return new ApiError(defaultMessageFor(httpStatus), { httpStatus });
  }

  const requestId = asString(rec.request_id);
  const err = asRecord(rec.error);
  if (!err) {
    // 非 2xx 却没有 error 字段：可能是被中间层拦截（网关错误页、代理超时），
    // 也可能撞上了不套信封的路径。按状态码给兜底文案，并保留原始体供排查。
    return new ApiError(defaultMessageFor(httpStatus), {
      httpStatus,
      requestId,
      body: rec,
    });
  }

  const details = asRecord(err.details) ?? undefined;
  const busy = details?.busy === true || err.code === "ESIM_BUSY";

  return new ApiError(asString(err.message) ?? defaultMessageFor(httpStatus), {
    httpStatus,
    code: asString(err.code),
    requestId,
    busy,
    retryAfterMs: asNumber(details?.retry_after_ms),
    reason: asString(details?.reason),
    details,
    body: rec,
  });
}

function defaultMessageFor(httpStatus: number): string {
  switch (httpStatus) {
    case 0:
      return "网络连接失败";
    case 401:
      return "未授权，请重新登录";
    case 403:
      return "没有权限";
    case 404:
      return "资源不存在";
    case 409:
      return "操作冲突，请稍后重试";
    case 429:
      return "请求过于频繁，请稍后再试";
    default:
      return httpStatus >= 500 ? "服务端错误" : "请求失败";
  }
}
