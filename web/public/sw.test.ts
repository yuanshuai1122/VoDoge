import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";
import { describe, expect, it, vi } from "vitest";

type WorkerHandler = (event: Record<string, unknown>) => void;

function loadServiceWorker(overrides?: {
  fetch?: ReturnType<typeof vi.fn>;
  cacheMatch?: ReturnType<typeof vi.fn>;
}) {
  const handlers = new Map<string, WorkerHandler>();
  const cache = {
    addAll: vi.fn().mockResolvedValue(undefined),
    put: vi.fn().mockResolvedValue(undefined),
  };
  const caches = {
    open: vi.fn().mockResolvedValue(cache),
    keys: vi
      .fn()
      .mockResolvedValue(["vodoge-shell-v1", "vodoge-shell-v2", "other-app"]),
    delete: vi.fn().mockResolvedValue(true),
    match: overrides?.cacheMatch ?? vi.fn().mockResolvedValue(undefined),
  };
  const self = {
    location: { origin: "https://vodoge.test" },
    clients: { claim: vi.fn().mockResolvedValue(undefined) },
    skipWaiting: vi.fn().mockResolvedValue(undefined),
    addEventListener(name: string, handler: WorkerHandler) {
      handlers.set(name, handler);
    },
  };
  const fetch = overrides?.fetch ?? vi.fn();
  const source = readFileSync(resolve(import.meta.dirname, "sw.js"), "utf8");

  vm.runInNewContext(source, { self, caches, fetch, URL });
  return { handlers, cache, caches, fetch };
}

function request(path: string, mode = "navigate") {
  return {
    method: "GET",
    mode,
    url: `https://vodoge.test${path}`,
    headers: { get: vi.fn().mockReturnValue("") },
  };
}

describe("service worker", () => {
  it("激活时只删除本应用的旧壳缓存", async () => {
    const { handlers, caches } = loadServiceWorker();
    let activation: Promise<unknown> | undefined;

    handlers.get("activate")?.({
      waitUntil(promise: Promise<unknown>) {
        activation = promise;
      },
    });
    await activation;

    expect(caches.delete).toHaveBeenCalledTimes(1);
    expect(caches.delete).toHaveBeenCalledWith("vodoge-shell-v1");
  });

  it("导航成功时按原请求缓存，不把任意页面覆盖到根路径", async () => {
    const response = { ok: true, clone: vi.fn().mockReturnValue({ copy: true }) };
    const fetchMock = vi.fn().mockResolvedValue(response);
    const { handlers, cache } = loadServiceWorker({ fetch: fetchMock });
    const req = request("/devices/dev-1");
    let handled: Promise<unknown> | undefined;

    handlers.get("fetch")?.({
      request: req,
      respondWith(promise: Promise<unknown>) {
        handled = promise;
      },
    });
    await handled;

    expect(cache.put).toHaveBeenCalledWith(req, { copy: true });
    expect(cache.put).not.toHaveBeenCalledWith("/", expect.anything());
  });

  it("离线导航先匹配同一路径，再回退到预缓存根页面", async () => {
    const offlineShell = { page: "root" };
    const cacheMatch = vi
      .fn()
      .mockResolvedValueOnce(undefined)
      .mockResolvedValueOnce(offlineShell);
    const { handlers } = loadServiceWorker({
      fetch: vi.fn().mockRejectedValue(new TypeError("offline")),
      cacheMatch,
    });
    const req = request("/sms/thread/10086");
    let handled: Promise<unknown> | undefined;

    handlers.get("fetch")?.({
      request: req,
      respondWith(promise: Promise<unknown>) {
        handled = promise;
      },
    });

    await expect(handled).resolves.toBe(offlineShell);
    expect(cacheMatch.mock.calls[0][0]).toBe(req);
    expect(cacheMatch.mock.calls[1][0]).toBe("/");
  });

  it("API 与 SSE 请求完全绕过缓存和 fetch 处理器", () => {
    const { handlers, caches, fetch } = loadServiceWorker();
    const respondWith = vi.fn();

    handlers.get("fetch")?.({
      request: request("/api/devices", "cors"),
      respondWith,
    });
    const sse = request("/events", "cors");
    sse.headers.get.mockReturnValue("text/event-stream");
    handlers.get("fetch")?.({ request: sse, respondWith });

    expect(respondWith).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
    expect(caches.match).not.toHaveBeenCalled();
  });
});
