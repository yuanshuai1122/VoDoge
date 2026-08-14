import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { downloadProfileStreamPath, startDownloadProfile } from "./esim";
import { ApiError } from "../errors";

/** 成功信封：{data, meta?, request_id} */
function okResponse(data: unknown, meta?: Record<string, unknown>): Response {
  return new Response(
    JSON.stringify({ data, ...(meta ? { meta } : {}), request_id: "r1" }),
    { status: 202, headers: { "Content-Type": "application/json" } },
  );
}

/** 错误信封：{error:{code,message,details?}, request_id} */
function errResponse(
  status: number,
  code: string,
  message: string,
  details?: Record<string, unknown>,
): Response {
  return new Response(
    JSON.stringify({
      error: { code, message, ...(details ? { details } : {}) },
      request_id: "r1",
    }),
    { status, headers: { "Content-Type": "application/json" } },
  );
}

describe("startDownloadProfile", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => vi.unstubAllGlobals());

  // 这是整条链路存在的理由：激活码必须留在请求体里，绝不能进 URL。
  // 一旦有人图省事改回 query，浏览器历史、Referer 和中间层日志都会留下它。
  it("激活参数走请求体，URL 里不出现", async () => {
    fetchMock.mockResolvedValue(okResponse({ task_id: "t1" }));

    await startDownloadProfile("dev1", {
      smdp: "rsp.example.com",
      matching_id: "MATCH-123",
      confirmation_code: "SECRET-CODE",
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/devices/dev1/esim/actions/download");
    expect(url).not.toContain("MATCH-123");
    expect(url).not.toContain("SECRET-CODE");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toMatchObject({
      smdp: "rsp.example.com",
      matching_id: "MATCH-123",
      confirmation_code: "SECRET-CODE",
    });
  });

  it("返回 task_id 且标记为新任务", async () => {
    fetchMock.mockResolvedValue(okResponse({ task_id: "t1" }));
    await expect(
      startDownloadProfile("dev1", { smdp: "x" }),
    ).resolves.toEqual({ task_id: "t1", already_running: false });
  });

  // 同一设备已有任务时后端回 409 + 那个任务的 id。前端应当接进去继续看进度，
  // 而不是把它当成错误弹出来——用户点两次按钮不该看到红字。
  it("409 带 task_id 时当作正常结果返回", async () => {
    fetchMock.mockResolvedValue(
      errResponse(409, "ESIM_DOWNLOAD_IN_PROGRESS", "该设备已有进行中的下载任务", {
        busy: true,
        task_id: "running-1",
      }),
    );

    await expect(startDownloadProfile("dev1", { smdp: "x" })).resolves.toEqual({
      task_id: "running-1",
      already_running: true,
    });
  });

  it("409 但没有 task_id 时仍然抛错", async () => {
    fetchMock.mockResolvedValue(errResponse(409, "conflict", "冲突"));
    await expect(
      startDownloadProfile("dev1", { smdp: "x" }),
    ).rejects.toBeInstanceOf(ApiError);
  });

  it("其它错误照常抛出", async () => {
    fetchMock.mockResolvedValue(errResponse(404, "not_found", "设备未找到"));
    await expect(
      startDownloadProfile("dev1", { smdp: "x" }),
    ).rejects.toMatchObject({ httpStatus: 404 });
  });

  it("设备 id 做 URL 编码", async () => {
    fetchMock.mockResolvedValue(okResponse({ task_id: "t" }));
    await startDownloadProfile("dev/1", { smdp: "x" });
    expect(fetchMock.mock.calls[0][0]).toContain("dev%2F1");
  });
});

describe("downloadProfileStreamPath", () => {
  // 订阅路径必须是 .../download/stream：SSE 的 ?token= 白名单按这条路径匹配，
  // 指错了会静默 401（重构前正是这样坏过一次）。
  it("指向 stream 子路径", () => {
    expect(downloadProfileStreamPath("dev1")).toBe(
      "/devices/dev1/esim/actions/download/stream",
    );
  });

  it("设备 id 做 URL 编码", () => {
    expect(downloadProfileStreamPath("a b")).toContain("a%20b");
  });
});
