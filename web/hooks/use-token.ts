"use client";

import { useSyncExternalStore } from "react";
import {
  subscribeToken,
  getTokenSnapshot,
  getTokenServerSnapshot,
} from "@/lib/auth/token";

/**
 * 订阅登录态。
 *
 * 用 useSyncExternalStore 而非 useState+useEffect：localStorage 是外部可变源，
 * 这样可以在 render 期间安全读取，既不会与预渲染结果错配，也避免了
 * 「effect 里 setState」导致的级联渲染。
 */
export function useToken(): string | null {
  return useSyncExternalStore(
    subscribeToken,
    getTokenSnapshot,
    getTokenServerSnapshot,
  );
}

/**
 * 是否已完成客户端 hydration。
 *
 * 预渲染时返回 false，客户端返回 true。用于那些必须等到浏览器环境才能
 * 正确渲染的内容（主题图标、依赖 localStorage 的提示）。
 */
export function useHydrated(): boolean {
  return useSyncExternalStore(
    () => () => {},
    () => true,
    () => false,
  );
}
