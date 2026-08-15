/**
 * PWA Service Worker 缓存策略（与 public/sw.js 保持一致）。
 *
 * 只缓存壳和静态资源。短信以服务器为准，/api、SSE、/ping 一律走网络。
 */
export function shouldBypassCache(url: URL, accept = ""): boolean {
  const path = url.pathname;
  if (path === "/ping" || path.startsWith("/api/") || path === "/api") {
    return true;
  }
  if (accept.includes("text/event-stream")) {
    return true;
  }
  return false;
}

export function isStaticShell(url: URL): boolean {
  if (shouldBypassCache(url)) return false;
  const path = url.pathname;
  if (path.startsWith("/_next/")) return true;
  if (path.startsWith("/icons/")) return true;
  return (
    path.endsWith(".js") ||
    path.endsWith(".css") ||
    path.endsWith(".svg") ||
    path.endsWith(".png") ||
    path.endsWith(".ico") ||
    path.endsWith(".webmanifest") ||
    path === "/manifest.webmanifest"
  );
}
