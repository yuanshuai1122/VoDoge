"use client";

import { useEffect, useState } from "react";
import { create } from "zustand";

/**
 * eSIM 操作互斥。
 *
 * 后端所有 eSIM 操作都经 APDU 仲裁器串行化，任一操作在占用期间会返回
 * 409 + {code:"ESIM_BUSY", retryAfterMs, reason} 并附 Retry-After 头。
 *
 * 这里维护两层锁：
 *  1. running —— 本前端自己发起的操作。乐观地禁用同设备其它入口，
 *     避免用户连点直接制造 409。
 *  2. cooldown —— 后端明确告知的忙碌窗口。可能来自其它客户端、Telegram bot
 *     或后台任务，因此即便本前端没在操作，也必须尊重这个窗口。
 *
 * TanStack Query 对 busy 错误不做自动重试（见 app/providers.tsx），
 * 重试时机完全由这里决定，以免多个请求同时争抢仲裁器。
 */

interface EsimLockState {
  /** deviceId -> 当前正在执行的操作名 */
  running: Record<string, string | undefined>;
  /** deviceId -> 后端要求的冷却截止时间戳(ms) 与原因 */
  cooldown: Record<string, { until: number; reason: string } | undefined>;

  begin: (deviceId: string, operation: string) => void;
  end: (deviceId: string) => void;
  markBusy: (deviceId: string, retryAfterMs?: number, reason?: string) => void;
  clearCooldown: (deviceId: string) => void;
}

/** 后端未给 retryAfterMs 时的保守默认值。 */
const DEFAULT_COOLDOWN_MS = 3000;

export const useEsimLockStore = create<EsimLockState>((set) => ({
  running: {},
  cooldown: {},

  begin: (deviceId, operation) =>
    set((s) => ({ running: { ...s.running, [deviceId]: operation } })),

  end: (deviceId) =>
    set((s) => ({ running: { ...s.running, [deviceId]: undefined } })),

  markBusy: (deviceId, retryAfterMs, reason) =>
    set((s) => ({
      cooldown: {
        ...s.cooldown,
        [deviceId]: {
          until: Date.now() + (retryAfterMs ?? DEFAULT_COOLDOWN_MS),
          reason: reason ?? "",
        },
      },
    })),

  clearCooldown: (deviceId) =>
    set((s) => ({ cooldown: { ...s.cooldown, [deviceId]: undefined } })),
}));

export interface EsimLockInfo {
  /** 是否应禁用该设备的所有 eSIM 操作入口 */
  locked: boolean;
  /** 本前端正在执行的操作名 */
  running?: string;
  /** 冷却剩余毫秒，用于倒计时展示 */
  remainingMs: number;
  reason?: string;
}

/**
 * 订阅某设备的 eSIM 锁状态。
 * 冷却期间按秒刷新，使倒计时可见并在到期后自动解除禁用。
 */
export function useEsimLock(deviceId: string): EsimLockInfo {
  const running = useEsimLockStore((s) => s.running[deviceId]);
  const cooldown = useEsimLockStore((s) => s.cooldown[deviceId]);
  const clearCooldown = useEsimLockStore((s) => s.clearCooldown);

  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!cooldown) return;

    const tick = () => {
      const current = Date.now();
      setNow(current);
      if (current >= cooldown.until) clearCooldown(deviceId);
    };

    const timer = setInterval(tick, 500);
    return () => clearInterval(timer);
  }, [cooldown, deviceId, clearCooldown]);

  const remainingMs = cooldown ? Math.max(0, cooldown.until - now) : 0;

  return {
    locked: Boolean(running) || remainingMs > 0,
    running,
    remainingMs,
    reason: cooldown?.reason,
  };
}
