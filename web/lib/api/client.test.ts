import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiFetch, buildUrl } from "./client";
import { ApiError } from "./errors";
import { setToken, clearToken } from "../auth/token";

/**
 * HTTP 客户端的横切行为：URL 拼装、鉴权头、超时、401 触发登出、响应体解析。
 * 这些每个请求都要走一遍，错一处就是全站故障，但没有一处能被 tsc 发现。
 */

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
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
    fetchMock.mockResolvedValue(jsonResponse(200, { status: "ok" }));

    await apiFetch("/devices");

    const headers = fetchMock.mock.calls[0][1].headers as Headers;
    expect(headers.get("Authorization")).toBe("Bearer tok-1");
  });

  it("无 token 时不带 Authorization 头", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, {}));
    await apiFetch("/ping");
    const headers = fetchMock.mock.calls[0][1].headers as Headers;
    expect(headers.get("Authorization")).toBeNull();
  });

  it("传 json 时序列化并设置 Content-Type", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, {}));
    await apiFetch("/sms/send", { method: "POST", json: { phone: "10086" } });

    const init = fetchMock.mock.calls[0][1];
    expect(init.body).toBe('{"phone":"10086"}');
    expect((init.headers as Headers).get("Content-Type")).toBe(
      "application/json",
    );
  });

  it("非 2xx 抛出归一化后的 ApiError", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(404, {
        status: "error",
        code: "not_found",
        message: "设备未找到",
        request_id: "r1",
      }),
    );

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
    fetchMock.mockResolvedValue(
      jsonResponse(401, { status: "error", message: "未授权" }),
    );

    await expect(apiFetch("/devices")).rejects.toBeInstanceOf(ApiError);
    expect(localStorage.getItem("vohive.token")).toBeNull();
  });

  // 登录页自己的请求 401 时不能触发登出，否则会打断正在输入的用户
  it("skipAuthRedirect 时 401 不登出", async () => {
    setToken("keep", new Date(Date.now() + 3600_000).toISOString());
    fetchMock.mockResolvedValue(
      jsonResponse(401, { status: "error", message: "密码错误" }),
    );

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

  it("204 与空体返回 null 而不是解析失败", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    await expect(apiFetch("/devices/x")).resolves.toBeNull();

    fetchMock.mockResolvedValue(new Response("", { status: 200 }));
    await expect(apiFetch("/devices/x")).resolves.toBeNull();
  });

  // 后端个别路径异常时返回纯文本，不能让 JSON.parse 把整个请求变成崩溃
  it("非 JSON 响应原样返回文本", async () => {
    fetchMock.mockResolvedValue(
      new Response("plain text", {
        status: 200,
        headers: { "Content-Type": "text/plain" },
      }),
    );
    await expect(apiFetch("/logs/history")).resolves.toBe("plain text");
  });

  it("声称是 JSON 但内容坏掉时退回文本", async () => {
    fetchMock.mockResolvedValue(
      new Response("{not json", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    await expect(apiFetch("/devices")).resolves.toBe("{not json");
  });
});
