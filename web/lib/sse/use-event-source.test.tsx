import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clearToken, setToken } from "../auth/token";
import { useEventSource } from "./use-event-source";

class MockEventSource {
  static instances: MockEventSource[] = [];

  readonly url: string;
  readonly listeners = new Map<string, EventListener>();
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  close = vi.fn();

  constructor(url: string | URL) {
    this.url = String(url);
    MockEventSource.instances.push(this);
  }

  addEventListener(name: string, listener: EventListener): void {
    this.listeners.set(name, listener);
  }

  removeEventListener(name: string): void {
    this.listeners.delete(name);
  }

  emitOpen(): void {
    this.onopen?.(new Event("open"));
  }

  emitError(): void {
    this.onerror?.(new Event("error"));
  }
}

function emitProbeThreshold(instance: MockEventSource): void {
  for (let i = 0; i < 5; i += 1) instance.emitError();
}

describe("useEventSource", () => {
  beforeEach(() => {
    MockEventSource.instances = [];
    vi.stubGlobal("EventSource", MockEventSource);
    setToken("session-token");
  });

  afterEach(() => {
    clearToken();
    vi.unstubAllGlobals();
  });

  it("用 path、query 和当前 token 建立连接，且 query 不能覆盖 token", () => {
    const onMessage = vi.fn();
    const { result } = renderHook(() =>
      useEventSource("/logs/stream", {
        events: { log: onMessage },
        query: { level: "warn", token: "caller-value" },
      }),
    );

    expect(result.current).toBe("connecting");
    expect(MockEventSource.instances).toHaveLength(1);
    expect(MockEventSource.instances[0].url).toBe(
      "/api/logs/stream?level=warn&token=session-token",
    );

    act(() => MockEventSource.instances[0].emitOpen());
    expect(result.current).toBe("open");
  });

  it("path 切换时立即回到 connecting 并关闭旧连接", () => {
    const events = { message: vi.fn() };
    const { result, rerender } = renderHook(
      ({ path }) => useEventSource(path, { events }),
      { initialProps: { path: "/logs/stream" } },
    );

    const first = MockEventSource.instances[0];
    act(() => first.emitOpen());
    expect(result.current).toBe("open");

    rerender({ path: "/devices/dev-2/overview/stream" });

    expect(result.current).toBe("connecting");
    expect(first.close).toHaveBeenCalledTimes(1);
    expect(MockEventSource.instances).toHaveLength(2);
  });

  it("连续失败探测到 401 时清除 token 并停止重连", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(null, {
        status: 401,
        headers: { "Content-Type": "text/event-stream" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() =>
      useEventSource("/logs/stream", { events: { log: vi.fn() } }),
    );
    const instance = MockEventSource.instances[0];

    act(() => emitProbeThreshold(instance));

    await waitFor(() => expect(localStorage.getItem("vodoge.token")).toBeNull());
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/logs/stream?token=session-token",
      expect.objectContaining({
        cache: "no-store",
        credentials: "omit",
        referrerPolicy: "no-referrer",
      }),
    );
    expect(instance.close).toHaveBeenCalled();
    expect(result.current).toBe("closed");
  });

  it("断网探测失败时保留 EventSource 的原生重连", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError("offline"));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() =>
      useEventSource("/logs/stream", { events: { log: vi.fn() } }),
    );
    const instance = MockEventSource.instances[0];

    act(() => emitProbeThreshold(instance));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    expect(instance.close).not.toHaveBeenCalled();
    act(() => instance.emitOpen());
    expect(result.current).toBe("open");
  });

  it("短暂限流不会被当成永久 4xx", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 429 }));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() =>
      useEventSource("/logs/stream", { events: { log: vi.fn() } }),
    );
    const instance = MockEventSource.instances[0];

    act(() => emitProbeThreshold(instance));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    expect(instance.close).not.toHaveBeenCalled();
    act(() => instance.emitOpen());
    expect(result.current).toBe("open");
  });

  it("卸载时中止尚未完成的状态探测", () => {
    let probeSignal: AbortSignal | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn((_url: string, init?: RequestInit) => {
        probeSignal = init?.signal ?? undefined;
        return new Promise<Response>(() => {});
      }),
    );
    const { unmount } = renderHook(() =>
      useEventSource("/logs/stream", { events: { log: vi.fn() } }),
    );

    act(() => emitProbeThreshold(MockEventSource.instances[0]));
    expect(probeSignal?.aborted).toBe(false);

    unmount();
    expect(probeSignal?.aborted).toBe(true);
  });
});
