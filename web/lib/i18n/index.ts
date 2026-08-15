"use client";

import { useSyncExternalStore } from "react";
import { en, zh, type MessageKey } from "./messages";

export type Locale = "zh" | "en";
export type { MessageKey };

const STORAGE_KEY = "vodog.locale";
const catalogs: Record<Locale, Record<MessageKey, string>> = { zh, en };

let locale: Locale = "zh";
const listeners = new Set<() => void>();

function readStored(): Locale {
  if (typeof window === "undefined") return "zh";
  try {
    const v = window.localStorage.getItem(STORAGE_KEY);
    if (v === "en" || v === "zh") return v;
  } catch {
    /* ignore */
  }
  return "zh";
}

if (typeof window !== "undefined") {
  locale = readStored();
}

function emit() {
  for (const fn of listeners) fn();
}

export function getLocale(): Locale {
  return locale;
}

export function setLocale(next: Locale) {
  if (next !== "zh" && next !== "en") return;
  locale = next;
  try {
    window.localStorage.setItem(STORAGE_KEY, next);
  } catch {
    /* ignore */
  }
  if (typeof document !== "undefined") {
    document.documentElement.lang = next === "zh" ? "zh-CN" : "en";
  }
  emit();
}

export function subscribeLocale(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

export function interpolate(
  template: string,
  vars?: Record<string, string | number>,
): string {
  if (!vars) return template;
  return template.replace(/\{(\w+)\}/g, (_, key: string) =>
    vars[key] == null ? `{${key}}` : String(vars[key]),
  );
}

export function t(
  key: MessageKey,
  vars?: Record<string, string | number>,
  loc: Locale = locale,
): string {
  const table = catalogs[loc] ?? zh;
  return interpolate(table[key] ?? zh[key] ?? key, vars);
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribeLocale, getLocale, () => "zh");
}

export function useT() {
  const loc = useLocale();
  return (
    key: MessageKey,
    vars?: Record<string, string | number>,
  ): string => t(key, vars, loc);
}

export function pluginLabel(
  loc: Locale,
  item: { label: string; label_zh?: string; label_en?: string },
): string {
  if (loc === "zh") return item.label_zh || item.label;
  return item.label_en || item.label;
}
