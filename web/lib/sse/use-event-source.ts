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
 *     浏览器会无限重试。这里用连续失败计数兜底，超过阈值即放弃并上报。
 *  2. token 出现在 URL 中。后端访问日志已脱敏，但浏览器历史/Referer 仍会留存，
 *     因此绝不要把这里构造的 URL 渲染成用户可见或可复制的链接。
 */

import { useEffect, useRef, useState } from "react";
import { API_BASE } from "../api/client";
import { getToken } from "../auth/token";

export type SSEStatus = "connecting" | "open" | "closed" | "error";

/** 连续失败多少次后放弃重连（浏览器默认约 3s 一次）。 */
const MAX_CONSECUTIVE_FAILURES = 5;

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

  // 仅在事件回调中更新，effect 体内不同步 setState
  const [connState, setConnState] = useState<Exclude<SSEStatus, "closed">>(
    "connecting",
  );

  // 处理函数存 ref，避免调用方每次渲染新建对象导致反复重连。
  // 更新必须放在 effect 里：render 期间写 ref 会破坏并发渲染的假设。
  const eventsRef = useRef(events);
  useEffect(() => {
    eventsRef.current = events;
  });

  const queryKey = JSON.stringify(query ?? {});
  const eventNames = Object.keys(events).sort().join(",");
  const active = Boolean(path) && enabled;

  useEffect(() => {
    if (!path || !enabled) return;

    // AuthGuard 保证进入受保护页面时有 token，这里只是防御性分支
    const token = getToken();
    if (!token) return;

    const sp = new URLSearchParams();
    const parsed = JSON.parse(queryKey) as Record<string, unknown>;
    for (const [k, v] of Object.entries(parsed)) {
      if (v === undefined || v === null || v === "") continue;
      sp.append(k, String(v));
    }
    sp.append("token", token);

    const es = new EventSource(`${API_BASE}/api${path}?${sp.toString()}`);
    let failures = 0;
    let disposed = false;

    es.onopen = () => {
      failures = 0;
      setConnState("open");
    };

    es.onerror = () => {
      if (disposed) return;
      failures += 1;
      // 401 不会让 EventSource 停下来，只能靠失败次数兜底
      if (failures >= MAX_CONSECUTIVE_FAILURES) {
        disposed = true;
        es.close();
        setConnState("error");
        return;
      }
      setConnState("connecting");
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
      for (const [name, listener] of listeners) {
        es.removeEventListener(name, listener);
      }
      // 必须 close：后端对 overview 流有 IncStreamSub 计数，
      // 泄漏连接会让服务端持续推送已卸载的页面
      es.close();
    };
  }, [path, enabled, queryKey, eventNames]);

  // 未启用时直接推导为 closed，不额外维护一份状态。
  // 切换 path 的瞬间可能短暂沿用上一条连接的状态，随首个 onopen/onerror 自动纠正。
  return active ? connState : "closed";
}
