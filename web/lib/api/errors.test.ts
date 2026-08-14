import { describe, expect, it } from "vitest";
import { ApiError, parseApiError } from "./errors";

/**
 * 错误归一化是前端与后端契约的接缝，而且 tsc 帮不上忙——响应体在类型系统里
 * 是 unknown，解析错了只会在运行时以"错误提示变成『请求失败』"的形式出现。
 */

function errBody(
  code: string,
  message: string,
  details?: Record<string, unknown>,
) {
  return {
    error: { code, message, ...(details ? { details } : {}) },
    request_id: "r1",
  };
}

describe("parseApiError", () => {
  it("解析 error.code / error.message 与顶层 request_id", () => {
    const err = parseApiError(404, errBody("not_found", "设备未找到"));

    expect(err).toBeInstanceOf(ApiError);
    expect(err.message).toBe("设备未找到");
    expect(err.code).toBe("not_found");
    expect(err.requestId).toBe("r1");
    expect(err.httpStatus).toBe(404);
    expect(err.busy).toBe(false);
  });

  // request_id 恒在，成功失败都有——用户报上来的错误要能在服务端日志里搜到。
  it("保留 request_id 供排查", () => {
    expect(parseApiError(500, errBody("internal_error", "boom")).requestId).toBe(
      "r1",
    );
  });

  it("非 2xx 却没有 error 字段时按状态码兜底，并保留原始体", () => {
    // 被网关错误页或代理超时截胡时会走到这里
    const err = parseApiError(502, { message: "Bad Gateway" });
    expect(err.message).toBe("服务端错误");
    expect(err.body).toEqual({ message: "Bad Gateway" });
  });

  it("响应体不是对象时按状态给默认文案", () => {
    expect(parseApiError(401, null).message).toBe("未授权，请重新登录");
    expect(parseApiError(0, "boom").message).toBe("网络连接失败");
    expect(parseApiError(503, []).message).toBe("服务端错误");
  });
});

describe("parseApiError eSIM 并发冲突", () => {
  const busy = errBody("ESIM_BUSY", "eSIM 操作正忙", {
    busy: true,
    reason: "switch_profile",
    retry_after_ms: 1200,
  });

  it("识别 busy 并取出重试等待与占用原因", () => {
    const err = parseApiError(409, busy);
    expect(err.busy).toBe(true);
    expect(err.code).toBe("ESIM_BUSY");
    expect(err.reason).toBe("switch_profile");
    expect(err.retryAfterMs).toBe(1200);
  });

  // 只认 snake_case：camelCase 的 retryAfterMs 已随信封改造删除。
  it("不再接受 camelCase 的 retryAfterMs", () => {
    const camel = errBody("ESIM_BUSY", "忙", { busy: true, retryAfterMs: 999 });
    expect(parseApiError(409, camel).retryAfterMs).toBeUndefined();
  });

  it("details 原样保留，供调用方取额外字段", () => {
    const err = parseApiError(
      409,
      errBody("ESIM_DOWNLOAD_IN_PROGRESS", "已有进行中的下载", {
        busy: true,
        task_id: "t1",
      }),
    );
    expect(err.details?.task_id).toBe("t1");
  });

  // code 是 ESIM_BUSY 就算 busy，即便 details 里没写——两个来源都认。
  it("仅凭 code 也能判定 busy", () => {
    expect(parseApiError(409, errBody("ESIM_BUSY", "忙")).busy).toBe(true);
  });
});

describe("ApiError 便捷判断", () => {
  it("isUnauthorized / isRateLimited / isNetworkError", () => {
    expect(new ApiError("x", { httpStatus: 401 }).isUnauthorized).toBe(true);
    expect(new ApiError("x", { httpStatus: 429 }).isRateLimited).toBe(true);
    // 0 表示网络层失败（断网、超时、CORS），与服务端返回的错误要能区分
    expect(new ApiError("x", { httpStatus: 0 }).isNetworkError).toBe(true);
    expect(new ApiError("x", { httpStatus: 500 }).isNetworkError).toBe(false);
  });
});
