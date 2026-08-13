import { describe, expect, it } from "vitest";
import { ApiError, parseApiError } from "./errors";

/**
 * 错误归一化是前端与后端契约的接缝，而且 tsc 帮不上忙——响应体在类型系统里
 * 是 unknown，解析错了只会在运行时以"错误提示变成'请求失败'"的形式出现。
 */

describe("parseApiError 现行形状", () => {
  it("解析 status/code/message/request_id", () => {
    const err = parseApiError(404, {
      status: "error",
      code: "not_found",
      message: "设备未找到",
      request_id: "abc123",
    });

    expect(err).toBeInstanceOf(ApiError);
    expect(err.message).toBe("设备未找到");
    expect(err.code).toBe("not_found");
    expect(err.requestId).toBe("abc123");
    expect(err.httpStatus).toBe(404);
    expect(err.busy).toBe(false);
  });

  // request_id 是这次统一的主要收益：用户报上来的错误要能在服务端日志里搜到。
  it("保留 request_id 供排查", () => {
    expect(
      parseApiError(500, {
        status: "error",
        code: "internal_error",
        message: "boom",
        request_id: "trace-1",
      }).requestId,
    ).toBe("trace-1");
  });
});

describe("parseApiError eSIM 并发冲突", () => {
  const busyBody = {
    status: "error",
    code: "ESIM_BUSY",
    message: "eSIM 操作正忙",
    request_id: "r1",
    busy: true,
    reason: "switch_profile",
    retry_after_ms: 1200,
    retryAfterMs: 1200,
  };

  it("识别 busy 并取出重试等待", () => {
    const err = parseApiError(409, busyBody);
    expect(err.busy).toBe(true);
    expect(err.code).toBe("ESIM_BUSY");
    expect(err.reason).toBe("switch_profile");
    expect(err.retryAfterMs).toBe(1200);
    expect(err.requestId).toBe("r1");
  });

  // snake_case 是现行字段名；camelCase 是兼容旧调用方保留的。两边都得认，
  // 否则调用方不知道该等多久，只能立刻重试再撞一次 409。
  it("只有 camelCase 时也能取到重试等待", () => {
    const camelOnly = { ...busyBody, retry_after_ms: undefined };
    expect(parseApiError(409, camelOnly).retryAfterMs).toBe(1200);
  });

  it("只有 snake_case 时也能取到", () => {
    const snakeOnly = { ...busyBody, retryAfterMs: undefined };
    expect(parseApiError(409, snakeOnly).retryAfterMs).toBe(1200);
  });

  // busy 分支必须先于普通分支判断，否则 busy/reason/retry 全部丢失。
  it("busy 优先于普通 message 分支", () => {
    const err = parseApiError(409, {
      status: "error",
      message: "正忙",
      busy: true,
      retry_after_ms: 800,
    });
    expect(err.busy).toBe(true);
    expect(err.retryAfterMs).toBe(800);
  });
});

describe("parseApiError 旧形状与兜底", () => {
  // 后端已不再产生裸 {error}，但外部脚本可能还在用；容错没有代价。
  it("仍认裸 {error:...}", () => {
    const err = parseApiError(400, { error: "smdp 为必填项" });
    expect(err.message).toBe("smdp 为必填项");
    expect(err.httpStatus).toBe(400);
  });

  it("响应体不是对象时按状态给默认文案", () => {
    expect(parseApiError(401, null).message).toBe("未授权，请重新登录");
    expect(parseApiError(0, "boom").message).toBe("网络连接失败");
    expect(parseApiError(503, []).message).toBe("服务端错误");
  });

  it("对象里没有可用字段时也给默认文案，且保留 request_id", () => {
    const err = parseApiError(500, { request_id: "r9" });
    expect(err.message).toBe("服务端错误");
    expect(err.requestId).toBe("r9");
  });

  it("原始响应体挂在 body 上供调用方取额外字段", () => {
    const err = parseApiError(409, {
      status: "error",
      code: "ESIM_DOWNLOAD_IN_PROGRESS",
      message: "已有进行中的下载",
      busy: true,
      task_id: "t1",
    });
    expect(err.body?.task_id).toBe("t1");
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
