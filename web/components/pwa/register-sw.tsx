"use client";

import { useEffect } from "react";

/**
 * 注册瘦 Service Worker。只在生产构建（静态导出）里跑，
 * next dev 下注册会把开发页缓存住，所以跳过。
 */
export function RegisterServiceWorker() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production") return;
    if (!("serviceWorker" in navigator)) return;
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // HTTP 局域网或浏览器不支持时安静失败；安装能力本来就要求 HTTPS
    });
  }, []);
  return null;
}
