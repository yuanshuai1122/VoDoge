"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useToken } from "@/hooks/use-token";

/**
 * 客户端路由守卫。
 *
 * 静态导出模式下没有服务端可做重定向，鉴权判断只能在客户端完成。
 * 登录态经 useSyncExternalStore 读取，因此 401 拦截、改密、其它标签页登出
 * 都会立即反映到这里，无需额外的事件订阅。
 */
export function AuthGuard({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const token = useToken();

  useEffect(() => {
    if (!token) router.replace("/login");
  }, [token, router]);

  // 未登录时不渲染受保护内容，避免跳转前的闪现
  if (!token) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <div className="text-sm text-muted-foreground">加载中…</div>
      </div>
    );
  }

  return <>{children}</>;
}
