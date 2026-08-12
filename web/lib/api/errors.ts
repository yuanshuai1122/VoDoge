/**
 * 错误响应归一化。
 *
 * 后端有三种错误形状（见 docs/frontend-api-matrix.md §2.2）：
 *   1. {status:"error", message, code?, request_id?}        —— 主流
 *   2. {error:"..."}                                        —— 整个 eSIM 模块
 *   3. {error, busy:true, code:"ESIM_BUSY", reason,
 *       retryAfterMs}                                       —— eSIM 并发冲突(409)
 *
 * 注意 retryAfterMs 是 camelCase，与全局 snake_case 相反。
 * `code` 字段仅在少数端点出现，业务层不要依赖它做分支，优先用 httpStatus。
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

  constructor(
    message: string,
    opts: {
      httpStatus: number;
      code?: string;
      requestId?: string;
      busy?: boolean;
      retryAfterMs?: number;
      reason?: string;
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

/** 把任意形状的错误响应体归一为 ApiError。 */
export function parseApiError(httpStatus: number, body: unknown): ApiError {
  const rec = asRecord(body);

  if (!rec) {
    return new ApiError(defaultMessageFor(httpStatus), { httpStatus });
  }

  // 形状 3：eSIM 并发冲突。必须在形状 2 之前判断——它同样带 error 字段。
  if (rec.busy === true || rec.code === "ESIM_BUSY") {
    return new ApiError(asString(rec.error) ?? "eSIM 操作正忙，请稍后重试", {
      httpStatus,
      code: asString(rec.code) ?? "ESIM_BUSY",
      busy: true,
      retryAfterMs: asNumber(rec.retryAfterMs),
      reason: asString(rec.reason),
    });
  }

  // 形状 1
  const message = asString(rec.message);
  if (message) {
    return new ApiError(message, {
      httpStatus,
      code: asString(rec.code),
      requestId: asString(rec.request_id),
    });
  }

  // 形状 2
  const err = asString(rec.error);
  if (err) {
    return new ApiError(err, { httpStatus, code: asString(rec.code) });
  }

  return new ApiError(defaultMessageFor(httpStatus), {
    httpStatus,
    requestId: asString(rec.request_id),
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
