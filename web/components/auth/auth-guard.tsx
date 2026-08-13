"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useToken, useHydrated } from "@/hooks/use-token";

/**
 * 客户端路由守卫。
 *
 * 静态导出模式下没有服务端可做重定向，鉴权判断只能在客户端完成。
 *
 * **必须等 hydration 完成后再判断**：预渲染出的 HTML 里 token 恒为 null
 * （localStorage 在构建期不存在），若在 hydration 首帧就跳转，会与登录页
 * 「已登录则跳回 /」的逻辑形成来回重定向，表现为组件反复挂载、请求风暴、
 * 页面内容渲染不出来。dev 模式下 hydration 时序不同，不会暴露此问题。
 */
export function AuthGuard({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const hydrated = useHydrated();
  const token = useToken();

  useEffect(() => {
    if (hydrated && !token) router.replace("/login");
  }, [hydrated, token, router]);

  if (!hydrated || !token) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <div className="text-sm text-muted-foreground">加载中…</div>
      </div>
    );
  }

  return <>{children}</>;
}
