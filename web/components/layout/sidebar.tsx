"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  Smartphone,
  Network,
  MessageSquare,
  ScrollText,
  Settings,
} from "lucide-react";
import { cn } from "@/lib/utils";

/** 一级导航，与后端能力对应。 */
const NAV = [
  { href: "/", label: "仪表盘", icon: LayoutDashboard },
  { href: "/devices", label: "设备管理", icon: Smartphone },
  { href: "/sms", label: "短信中心", icon: MessageSquare },
  { href: "/proxy", label: "代理管理", icon: Network },
  { href: "/logs", label: "实时日志", icon: ScrollText },
  { href: "/settings", label: "系统设置", icon: Settings },
] as const;

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="hidden w-56 shrink-0 border-r bg-sidebar md:flex md:flex-col">
      <div className="flex h-14 items-center gap-2 border-b px-4">
        <span className="text-base font-semibold tracking-tight">VoHive</span>
      </div>

      <nav className="flex flex-1 flex-col gap-1 p-2">
        {NAV.map(({ href, label, icon: Icon }) => {
          // 根路径必须精确匹配，否则每个子路由都会把「仪表盘」点亮
          const active =
            href === "/" ? pathname === "/" : pathname.startsWith(href);

          return (
            <Link
              key={href}
              href={href}
              className={cn(
                "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm transition-colors",
                active
                  ? "bg-sidebar-accent text-sidebar-accent-foreground font-medium"
                  : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
              )}
            >
              <Icon className="size-4 shrink-0" />
              {label}
            </Link>
          );
        })}
      </nav>
    </aside>
  );
}
