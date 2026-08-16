"use client";

/**
 * SSE 订阅 hook。
 *
 * 后端自 2026-08-12 起对 4 个流式端点开放 ?token= 回退（白名单见 server.go 的
 * sseTokenQueryRoutes），因此可直接使用原生 EventSource，重连与 Last-Event-ID
 * 由浏览器负责，无需自研解析器。
 *
 * 两个坑：
 *  1. EventSource 不会因为 401 停止重连 —— 后端对未授权的 SSE 请求返回 401 后关闭连接，
 *     浏览器会无限重试。这里在连续失败后探测状态，只对永久 4xx 停止重连。
 *  2. token 出现在 URL 中。后端访问日志已脱敏，但浏览器历史/Referer 仍会留存，
 *     因此绝不要把这里构造的 URL 渲染成用户可见或可复制的链接。
 */

import { useEffect, useRef, useState } from "react";
import { API_BASE } from "../api/client";
import { triggerLogout } from "../auth/token";
import { useToken } from "@/hooks/use-token";

/**
 * SSE 专用的 base，与普通请求不同。
 *
 * 生产同源，走 API_BASE（通常为空）即可。
 * 开发期必须**绕开 Next dev 的 rewrite 代理**：它会缓冲 SSE 响应体，
 * 经代理订阅时 status 200、content-type 正确，但一个字节都收不到。
 * 直连后端需要 CORS，后端仅在 `server.debug: true` 时放行 localhost。
 */
const SSE_BASE =
  process.env.NEXT_PUBLIC_SSE_BASE ??
  (process.env.NODE_ENV === "development" ? "http://127.0.0.1:7575" : API_BASE);

export type SSEStatus = "connecting" | "open" | "closed" | "error";

/** 连续失败多少次后探测响应状态（浏览器默认约 3s 一次）。 */
const MAX_CONSECUTIVE_FAILURES = 5;
const AUTH_PROBE_TIMEOUT_MS = 5_000;

function isPermanentClientError(status: number): boolean {
  return (
    status >= 400 &&
    status < 500 &&
    status !== 408 &&
    status !== 425 &&
    status !== 429
  );
}

export interface UseEventSourceOptions {
  /** 事件名 -> 处理函数。eSIM 下载流无 event 行，用 "message"。 */
  events: Record<string, (data: unknown) => void>;
  /** 为 false 时不建立连接（如设备未选中）。 */
  enabled?: boolean;
  query?: Record<string, string | number | undefined>;
}

/**
 * @param path 形如 "/devices/abc/overview/stream"，不含 /api 前缀
 */
export function useEventSource(
  path: string | null,
  options: UseEventSourceOptions,
): SSEStatus {
  const { events, enabled = true, query } = options;
  const token = useToken();

  const [connState, setConnState] = useState<{
    key: string;
    status: Exclude<SSEStatus, "closed">;
  }>({ key: "", status: "connecting" });

  // 处理函数存 ref，避免调用方每次渲染新建对象导致反复重连。
  // 更新必须放在 effect 里：render 期间写 ref 会破坏并发渲染的假设。
  const eventsRef = useRef(events);
  useEffect(() => {
    eventsRef.current = events;
  });

  const queryKey = JSON.stringify(query ?? {});
  const eventNames = Object.keys(events).sort().join(",");
  const active = Boolean(path) && enabled && Boolean(token);
  const connectionKey = active
    ? `${path}\u0000${queryKey}\u0000${eventNames}\u0000${token}`
    : "";

  useEffect(() => {
    if (!path || !enabled || !token) return;

    const sp = new URLSearchParams();
    const parsed = JSON.parse(queryKey) as Record<string, unknown>;
    for (const [k, v] of Object.entries(parsed)) {
      if (v === undefined || v === null || v === "") continue;
      sp.append(k, String(v));
    }
    // 调用方的 query 不能覆盖认证凭证。
    sp.set("token", token);

    const eventURL = `${SSE_BASE}/api${path}?${sp.toString()}`;
    const es = new EventSource(eventURL);
    let failures = 0;
    let disposed = false;
    let probing = false;
    let probeController: AbortController | null = null;
    let probeTimeout: number | null = null;

    es.onopen = () => {
      failures = 0;
      setConnState({ key: connectionKey, status: "open" });
    };

    es.onerror = () => {
      if (disposed) return;
      failures += 1;
      setConnState({ key: connectionKey, status: "connecting" });
      if (failures < MAX_CONSECUTIVE_FAILURES || probing) return;

      // EventSource hides the HTTP status. Probe the same endpoint after repeated
      // failures: a permanent 4xx should stop, while offline/5xx must keep the
      // browser's native retry loop alive.
      probing = true;
      const controller = new AbortController();
      probeController = controller;
      probeTimeout = window.setTimeout(
        () => controller.abort(),
        AUTH_PROBE_TIMEOUT_MS,
      );
      void fetch(eventURL, {
        method: "GET",
        headers: { Accept: "text/event-stream" },
        cache: "no-store",
        credentials: "omit",
        referrerPolicy: "no-referrer",
        signal: controller.signal,
      })
        .then(async (response) => {
          await response.body?.cancel().catch(() => {});
          if (disposed) return;
          if (isPermanentClientError(response.status)) {
            disposed = true;
            es.close();
            setConnState({ key: connectionKey, status: "error" });
            if (response.status === 401) triggerLogout();
            return;
          }
          if (response.ok) failures = 0;
        })
        .catch(() => {
          // Network failure is transient. EventSource remains responsible for retry.
        })
        .finally(() => {
          if (probeController !== controller) return;
          if (probeTimeout !== null) window.clearTimeout(probeTimeout);
          probeController = null;
          probeTimeout = null;
          probing = false;
        });
    };

    const listeners: Array<[string, EventListener]> = [];
    for (const name of eventNames.split(",").filter(Boolean)) {
      const listener: EventListener = (evt) => {
        const me = evt as MessageEvent;
        let data: unknown = me.data;
        try {
          data = JSON.parse(me.data);
        } catch {
          // 少数事件可能不是 JSON，原样透传
        }
        eventsRef.current[name]?.(data);
      };
      es.addEventListener(name, listener);
      listeners.push([name, listener]);
    }

    return () => {
      disposed = true;
      probeController?.abort();
      if (probeTimeout !== null) window.clearTimeout(probeTimeout);
      probeController = null;
      probeTimeout = null;
      for (const [name, listener] of listeners) {
        es.removeEventListener(name, listener);
      }
      // 必须 close：后端对 overview 流有 IncStreamSub 计数，
      // 泄漏连接会让服务端持续推送已卸载的页面
      es.close();
    };
  }, [path, enabled, queryKey, eventNames, token, connectionKey]);

  // 未启用时直接推导为 closed，不额外维护一份状态。
  // connectionKey 不匹配时立即显示 connecting，不能沿用上一条连接的 open。
  if (!active) return "closed";
  return connState.key === connectionKey ? connState.status : "connecting";
}
