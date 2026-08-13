/**
 * 错误响应归一化。
 *
 * 后端自 2026-08-14 起只有一种错误形状：
 *   {status:"error", code, message, request_id}
 * 并发冲突等场景在同一层级附加 busy / reason / retry_after_ms / task_id。
 *
 * 曾经还有裸 {error:"..."}（整个 eSIM 模块、卡策略、选网）。**解析仍然保留**
 * 那一支：外部脚本可能仍按旧形状发请求或解析响应，容错本身没有代价，
 * 而少一个分支省不下什么。
 *
 * `code` 多数时候只是按 HTTP 状态推导的通用码，业务层优先用 httpStatus 分支；
 * 只有 ESIM_BUSY / ESIM_DOWNLOAD_IN_PROGRESS / e911_* / websheet_* 这类
 * 专属码值得直接判。
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
   * 原始错误体。少数端点会在错误响应里带上调用方需要的数据——例如
   * eSIM 下载的 409 会返回进行中任务的 task_id，调用方要用它去订阅进度。
   * 归一化后的字段之外的东西都在这里，取用时自行做类型收窄。
   */
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

/** 把任意形状的错误响应体归一为 ApiError。 */
export function parseApiError(httpStatus: number, body: unknown): ApiError {
  const rec = asRecord(body);

  if (!rec) {
    return new ApiError(defaultMessageFor(httpStatus), { httpStatus });
  }

  // 并发冲突先判：它同时带 message 与（历史上的）error，落到下面会丢掉
  // busy / retry_after_ms，调用方就不知道该等多久了。
  if (rec.busy === true || rec.code === "ESIM_BUSY") {
    return new ApiError(
      asString(rec.message) ?? asString(rec.error) ?? "eSIM 操作正忙，请稍后重试",
      {
        httpStatus,
        code: asString(rec.code) ?? "ESIM_BUSY",
        busy: true,
        // snake_case 是现行字段名，camelCase 是为兼容保留的旧名
        retryAfterMs:
          asNumber(rec.retry_after_ms) ?? asNumber(rec.retryAfterMs),
        reason: asString(rec.reason),
        requestId: asString(rec.request_id),
        body: rec,
      },
    );
  }

  // 现行形状
  const message = asString(rec.message);
  if (message) {
    return new ApiError(message, {
      httpStatus,
      code: asString(rec.code),
      requestId: asString(rec.request_id),
      body: rec,
    });
  }

  // 旧形状：裸 {error:"..."}。后端已不再产生，保留以兼容外部调用方。
  const err = asString(rec.error);
  if (err) {
    return new ApiError(err, {
      httpStatus,
      code: asString(rec.code),
      requestId: asString(rec.request_id),
      body: rec,
    });
  }

  return new ApiError(defaultMessageFor(httpStatus), {
    httpStatus,
    requestId: asString(rec.request_id),
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
