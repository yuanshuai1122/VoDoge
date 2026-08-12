import type { NextConfig } from "next";

// 开发期：next dev 独立跑（默认 :3000），/api/* 反代到 Go 后端，避免 CORS。
// 生产期：next build 静态导出到 web/dist，由 Dockerfile / Makefile 拷入
// internal/web/dist 供 Go 嵌入托管（后端已有 SPA fallback，见 internal/api/server.go）。
const isProd = process.env.NODE_ENV === "production";
const apiBase = process.env.NEXT_PUBLIC_API_BASE ?? "http://127.0.0.1:7575";

const nextConfig: NextConfig = isProd
  ? {
      output: "export",
      // 静态导出模式下 distDir 即导出目录，对齐构建脚本期望的 web/dist
      distDir: "dist",
      // 静态导出无图片优化服务
      images: { unoptimized: true },
    }
  : {
      async rewrites() {
        return [
          {
            source: "/api/:path*",
            destination: `${apiBase}/api/:path*`,
          },
        ];
      },
    };

export default nextConfig;
