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

const NAV = [
  { href: "/", label: "首页", icon: LayoutDashboard },
  { href: "/devices", label: "设备", icon: Smartphone },
  { href: "/sms", label: "短信", icon: MessageSquare },
  { href: "/proxy", label: "代理", icon: Network },
  { href: "/logs", label: "日志", icon: ScrollText },
  { href: "/settings", label: "设置", icon: Settings },
] as const;

export function MobileNav() {
  const pathname = usePathname();

  return (
    <nav
      aria-label="主导航"
      className="fixed inset-x-0 bottom-0 z-40 border-t bg-background/95 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden"
    >
      <ul className="grid grid-cols-6">
        {NAV.map(({ href, label, icon: Icon }) => {
          const active =
            href === "/" ? pathname === "/" : pathname.startsWith(href);
          return (
            <li key={href}>
              <Link
                href={href}
                className={cn(
                  "flex flex-col items-center gap-0.5 px-1 py-2 text-[11px]",
                  active
                    ? "text-foreground font-medium"
                    : "text-muted-foreground",
                )}
              >
                <Icon className="size-4" />
                {label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
