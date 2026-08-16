/**
 * Token 存储。
 *
 * 后端签发的是无状态 token：base64(exp + "." + HMAC(密码, exp))，有效期 30 天，
 * 无 refresh 机制。HMAC 密钥就是登录密码本身，因此**改密后所有既有 token 立即失效**——
 * 改密成功后必须 triggerLogout()，否则会陷入「界面已登录但请求全 401」。
 *
 * 对外以 useSyncExternalStore 的形式暴露（见 hooks/use-token.ts）：
 * 组件可以在 render 期间安全读取，无需 effect + setState，也不会有 hydration 错配。
 */

const TOKEN_KEY = "vodoge.token";
const EXPIRES_KEY = "vodoge.token.expires_at";

const listeners = new Set<() => void>();

function notify(): void {
  listeners.forEach((fn) => {
    try {
      fn();
    } catch {
      // 单个监听器异常不影响其它监听器
    }
  });
}

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

/** @param expiresAt 后端返回的 RFC3339 字符串 */
export function setToken(token: string, expiresAt?: string): void {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(TOKEN_KEY, token);
  if (expiresAt) window.localStorage.setItem(EXPIRES_KEY, expiresAt);
  notify();
}

export function clearToken(): void {
  if (typeof window === "undefined") return;
  window.localStorage.removeItem(TOKEN_KEY);
  window.localStorage.removeItem(EXPIRES_KEY);
  notify();
}

/** 清凭证并广播。401 拦截与主动登出共用；无状态 bearer 本身不能服务端撤销。 */
export function triggerLogout(): void {
  clearToken();
}

export function getExpiresAt(): Date | null {
  if (typeof window === "undefined") return null;
  const raw = window.localStorage.getItem(EXPIRES_KEY);
  if (!raw) return null;
  const d = new Date(raw);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** 30 天有效期无法续期，临期只能提示用户重新登录。 */
export function isExpiringWithin(days: number): boolean {
  const exp = getExpiresAt();
  if (!exp) return false;
  return exp.getTime() - Date.now() < days * 24 * 60 * 60 * 1000;
}

// ---- useSyncExternalStore 接口 ----

export function subscribeToken(onChange: () => void): () => void {
  listeners.add(onChange);
  // 跨标签页：另一个标签页登出后，本标签页也应失效
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

export const getTokenSnapshot = getToken;

/** 预渲染阶段没有 localStorage，一律视为未登录。 */
export function getTokenServerSnapshot(): string | null {
  return null;
}
