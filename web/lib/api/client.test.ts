import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiFetch, buildUrl } from "./client";
import { ApiError } from "./errors";
import { setToken, clearToken } from "../auth/token";

/**
 * HTTP 客户端的横切行为：URL 拼装、鉴权头、超时、401 触发登出、响应体解析。
 * 这些每个请求都要走一遍，错一处就是全站故障，但没有一处能被 tsc 发现。
 */

/** 成功信封 */
function okResponse(data: unknown, meta?: Record<string, unknown>): Response {
  return new Response(
    JSON.stringify({ data, ...(meta ? { meta } : {}), request_id: "r1" }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}

/** 错误信封 */
function errResponse(status: number, code: string, message: string): Response {
  return new Response(
    JSON.stringify({ error: { code, message }, request_id: "r1" }),
    { status, headers: { "Content-Type": "application/json" } },
  );
}

describe("buildUrl", () => {
  it("补上 /api 前缀并允许省略前导斜杠", () => {
    expect(buildUrl("/devices")).toBe("/api/devices");
    expect(buildUrl("devices")).toBe("/api/devices");
  });

  // undefined/null/"" 一律跳过，否则会拼出 ?peer=undefined 这种查询
  it("跳过空查询参数", () => {
    expect(
      buildUrl("/sms/thread", {
        peer: "10086",
        before: undefined,
        cursor: null,
        q: "",
        limit: 20,
      }),
    ).toBe("/api/sms/thread?peer=10086&limit=20");
  });

  it("无查询参数时不带问号", () => {
    expect(buildUrl("/devices", {})).toBe("/api/devices");
  });
});

describe("apiFetch", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    clearToken();
    vi.unstubAllGlobals();
  });

  it("有 token 时带上 Authorization 头", async () => {
    setToken("tok-1", new Date(Date.now() + 3600_000).toISOString());
    fetchMock.mockResolvedValue(okResponse(null));

    await apiFetch("/devices");

    const headers = fetchMock.mock.calls[0][1].headers as Headers;
    expect(headers.get("Authorization")).toBe("Bearer tok-1");
  });

  it("无 token 时不带 Authorization 头", async () => {
    fetchMock.mockResolvedValue(okResponse(null));
    await apiFetch("/ping");
    const headers = fetchMock.mock.calls[0][1].headers as Headers;
    expect(headers.get("Authorization")).toBeNull();
  });

  it("传 json 时序列化并设置 Content-Type", async () => {
    fetchMock.mockResolvedValue(okResponse(null));
    await apiFetch("/sms/send", { method: "POST", json: { phone: "10086" } });

    const init = fetchMock.mock.calls[0][1];
    expect(init.body).toBe('{"phone":"10086"}');
    expect((init.headers as Headers).get("Content-Type")).toBe(
      "application/json",
    );
  });

  it("非 2xx 抛出归一化后的 ApiError", async () => {
    fetchMock.mockResolvedValue(errResponse(404, "not_found", "设备未找到"));

    await expect(apiFetch("/devices/x")).rejects.toMatchObject({
      httpStatus: 404,
      code: "not_found",
      message: "设备未找到",
      requestId: "r1",
    });
  });

  // token 失效最常见的来源是改密（HMAC 密钥就是密码），必须清掉本地状态，
  // 否则用户会卡在一个所有请求都 401 的界面上。
  it("401 触发全局登出", async () => {
    setToken("stale", new Date(Date.now() + 3600_000).toISOString());
    fetchMock.mockResolvedValue(errResponse(401, "unauthorized", "未授权"));

    await expect(apiFetch("/devices")).rejects.toBeInstanceOf(ApiError);
    expect(localStorage.getItem("vohive.token")).toBeNull();
  });

  // 登录页自己的请求 401 时不能触发登出，否则会打断正在输入的用户
  it("skipAuthRedirect 时 401 不登出", async () => {
    setToken("keep", new Date(Date.now() + 3600_000).toISOString());
    fetchMock.mockResolvedValue(errResponse(401, "unauthorized", "密码错误"));

    await expect(
      apiFetch("/auth/login", { skipAuthRedirect: true }),
    ).rejects.toBeInstanceOf(ApiError);
    expect(localStorage.getItem("vohive.token")).not.toBeNull();
  });

  it("网络失败归一为 httpStatus 0", async () => {
    fetchMock.mockRejectedValue(new TypeError("failed to fetch"));
    await expect(apiFetch("/devices")).rejects.toMatchObject({
      httpStatus: 0,
      message: "网络连接失败",
    });
  });

  it("超时归一为 httpStatus 0 且文案区分于断网", async () => {
    fetchMock.mockRejectedValue(
      new DOMException("aborted", "AbortError"),
    );
    await expect(apiFetch("/devices")).rejects.toMatchObject({
      httpStatus: 0,
      message: "请求超时",
    });
  });

  it("拆开信封，data 与 meta 分开返回", async () => {
    fetchMock.mockResolvedValue(
      okResponse([{ id: "dev1" }], { device_limit: 3 }),
    );

    const res = await apiFetch<Array<{ id: string }>>("/devices");
    expect(res.data).toEqual([{ id: "dev1" }]);
    expect(res.meta.device_limit).toBe(3);
    expect(res.requestId).toBe("r1");
  });

  it("没有 meta 时给空对象，调用方不必判空", async () => {
    fetchMock.mockResolvedValue(okResponse({ id: "dev1" }));
    expect((await apiFetch("/devices/dev1/overview")).meta).toEqual({});
  });

  it("data 为 null 的纯动作响应也能正常拆开", async () => {
    fetchMock.mockResolvedValue(okResponse(null, { message: "已提交" }));
    const res = await apiFetch("/devices/dev1/actions/reboot");
    expect(res.data).toBeNull();
    expect(res.meta.message).toBe("已提交");
  });

  // 2xx 却没有 data：漏改的端点，或者中间层插了一脚。
  // 静默当成载荷会让问题一路飘到渲染层，不如在此处就说清楚。
  it("2xx 但不符合信封结构时抛错而不是静默透传", async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ devices: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    await expect(apiFetch("/devices")).rejects.toThrow(/信封/);
  });

  it("204 与空体返回 null 而不是解析失败", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    expect((await apiFetch("/devices/x")).data).toBeNull();

    fetchMock.mockResolvedValue(new Response("", { status: 200 }));
    expect((await apiFetch("/devices/x")).data).toBeNull();
  });

  // 不套信封的少数路径（纯文本日志等）原样透传，不能让 JSON.parse 变成崩溃
  it("非 JSON 响应原样透传", async () => {
    fetchMock.mockResolvedValue(
      new Response("plain text", {
        status: 200,
        headers: { "Content-Type": "text/plain" },
      }),
    );
    expect((await apiFetch("/logs/history")).data).toBe("plain text");
  });

  it("声称是 JSON 但内容坏掉时退回文本", async () => {
    fetchMock.mockResolvedValue(
      new Response("{not json", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    expect((await apiFetch("/devices")).data).toBe("{not json");
  });
});
