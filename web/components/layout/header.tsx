"use client";

import { useTheme } from "next-themes";
import { Moon, Sun, LogOut, AlertTriangle, Eye, EyeOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { logout } from "@/lib/api/endpoints/auth";
import { isExpiringWithin } from "@/lib/auth/token";
import { useHydrated } from "@/hooks/use-token";
import {
  useRevealSensitive,
  toggleRevealSensitive,
} from "@/components/common/sensitive";

export function Header() {
  const { resolvedTheme, setTheme } = useTheme();
  // 主题与 localStorage 都只在浏览器可用，hydration 完成前按预渲染结果渲染
  const hydrated = useHydrated();
  const reveal = useRevealSensitive();

  // token 有效期 30 天且后端无 refresh 机制，临期只能提示重新登录
  const expiringSoon = hydrated && isExpiringWithin(3);

  return (
    <header className="flex h-14 shrink-0 items-center justify-between gap-4 border-b px-4">
      <div className="flex items-center gap-2 md:hidden">
        <span className="font-semibold">VoDog</span>
      </div>

      <div className="flex flex-1 items-center justify-end gap-2">
        {expiringSoon && (
          <span className="flex items-center gap-1.5 text-xs text-amber-600 dark:text-amber-500">
            <AlertTriangle className="size-3.5" />
            登录即将过期，请重新登录
          </span>
        )}

        <Button
          variant="ghost"
          size="icon"
          aria-label={reveal ? "隐藏敏感标识" : "显示敏感标识"}
          title={reveal ? "隐藏 IMEI/ICCID/IMSI" : "显示 IMEI/ICCID/IMSI"}
          onClick={toggleRevealSensitive}
        >
          {reveal ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
        </Button>

        <Button
          variant="ghost"
          size="icon"
          aria-label="切换主题"
          onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
        >
          {hydrated && resolvedTheme === "dark" ? (
            <Sun className="size-4" />
          ) : (
            <Moon className="size-4" />
          )}
        </Button>

        <Button variant="ghost" size="icon" aria-label="退出登录" onClick={logout}>
          <LogOut className="size-4" />
        </Button>
      </div>
    </header>
  );
}
