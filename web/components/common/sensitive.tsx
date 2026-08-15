"use client";

import { useSyncExternalStore } from "react";
import { maskIdentifier } from "@/lib/format";

/**
 * 敏感标识（IMEI / ICCID / IMSI / EID）的显隐控制。
 *
 * 默认打码：这些值足以定位一张实体卡或一台设备，而管理台常被截图分享。
 * 开关存 localStorage 并通过 useSyncExternalStore 广播，使所有实例同步切换，
 * 且不需要在 effect 里 setState。
 */

const KEY = "vodog.reveal_sensitive";
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((fn) => fn());
}

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange);
  if (typeof window !== "undefined") {
    window.addEventListener("storage", onChange);
  }
  return () => {
    listeners.delete(onChange);
    if (typeof window !== "undefined") {
      window.removeEventListener("storage", onChange);
    }
  };
}

function getSnapshot(): boolean {
  if (typeof window === "undefined") return false;
  return window.localStorage.getItem(KEY) === "1";
}

/** 预渲染阶段一律按打码输出，避免与客户端首帧不一致。 */
function getServerSnapshot(): boolean {
  return false;
}

export function useRevealSensitive(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function toggleRevealSensitive(): void {
  if (typeof window === "undefined") return;
  const next = getSnapshot() ? "0" : "1";
  window.localStorage.setItem(KEY, next);
  notify();
}

/** 按当前显隐设置渲染一个敏感标识。 */
export function Sensitive({
  value,
  visible = 4,
  className,
}: {
  value: string | undefined | null;
  /** 打码时首尾各保留几位，便于人工核对 */
  visible?: number;
  className?: string;
}) {
  const reveal = useRevealSensitive();
  if (!value) return <span className={className}>-</span>;
  return (
    <span className={className}>
      {reveal ? value : maskIdentifier(value, visible)}
    </span>
  );
}
