/* VoDoge PWA：只缓存壳和静态资源。
 * /api/*、/ping、text/event-stream 一律不拦截，走浏览器默认网络。
 * 规则与 web/lib/pwa/cache-policy.ts 对齐。
 */
const CACHE = "vodoge-shell-v1";
const PRECACHE = ["/", "/sms", "/login", "/manifest.webmanifest", "/icons/icon.svg"];

function bypass(url, accept) {
  const path = url.pathname;
  if (path === "/ping" || path.startsWith("/api/") || path === "/api") return true;
  if (accept && accept.indexOf("text/event-stream") !== -1) return true;
  return false;
}

function isStatic(url) {
  if (bypass(url, "")) return false;
  const path = url.pathname;
  if (path.indexOf("/_next/") === 0 || path.indexOf("/icons/") === 0) return true;
  return (
    /\.(js|css|svg|png|ico|webmanifest)$/.test(path) ||
    path === "/manifest.webmanifest"
  );
}

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE).then((cache) => cache.addAll(PRECACHE)).then(() => self.skipWaiting()),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))),
      )
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;

  let url;
  try {
    url = new URL(req.url);
  } catch {
    return;
  }
  if (url.origin !== self.location.origin) return;

  const accept = req.headers.get("accept") || "";
  if (bypass(url, accept)) return;

  if (req.mode === "navigate") {
    event.respondWith(
      fetch(req)
        .then((res) => {
          const copy = res.clone();
          caches.open(CACHE).then((cache) => cache.put("/", copy)).catch(() => {});
          return res;
        })
        .catch(() => caches.match(req).then((hit) => hit || caches.match("/"))),
    );
    return;
  }

  if (!isStatic(url)) return;

  event.respondWith(
    caches.match(req).then((hit) => {
      if (hit) return hit;
      return fetch(req).then((res) => {
        if (res && res.ok) {
          const copy = res.clone();
          caches.open(CACHE).then((cache) => cache.put(req, copy)).catch(() => {});
        }
        return res;
      });
    }),
  );
});
