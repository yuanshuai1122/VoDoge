"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  LayoutDashboard,
  Puzzle,
  Smartphone,
  Network,
  MessageSquare,
  ScrollText,
  Settings,
} from "lucide-react";
import { cn } from "@/lib/utils";
import {
  listPlugins,
  pluginPageURL,
} from "@/lib/api/endpoints/extensions";

/** 一级导航，与后端能力对应。 */
const NAV = [
  { href: "/", label: "仪表盘", icon: LayoutDashboard, key: "dashboard" },
  { href: "/devices", label: "设备管理", icon: Smartphone, key: "devices" },
  { href: "/sms", label: "短信中心", icon: MessageSquare, key: "sms" },
  { href: "/proxy", label: "代理管理", icon: Network, key: "proxy" },
  { href: "/logs", label: "实时日志", icon: ScrollText, key: "logs" },
  { href: "/settings", label: "系统设置", icon: Settings, key: "settings" },
] as const;

export function Sidebar() {
  return (
    <aside className="hidden w-56 shrink-0 border-r bg-sidebar md:flex md:flex-col">
      <div className="flex h-14 items-center gap-2 border-b px-4">
        <span className="text-base font-semibold tracking-tight">VoDog</span>
      </div>
      <Suspense
        fallback={
          <nav className="flex flex-1 flex-col gap-1 p-2">
            {NAV.map(({ href, label, icon: Icon }) => (
              <span
                key={href}
                className="flex items-center gap-2.5 rounded-md px-3 py-2 text-sm text-muted-foreground"
              >
                <Icon className="size-4 shrink-0" />
                {label}
              </span>
            ))}
          </nav>
        }
      >
        <SidebarNav />
      </Suspense>
    </aside>
  );
}

function SidebarNav() {
  const pathname = usePathname();
  const search = useSearchParams();
  const plugins = useQuery({
    queryKey: ["extensions"],
    queryFn: listPlugins,
  });

  const items = buildNavItems(
    (plugins.data ?? []).flatMap((plugin) =>
      plugin.enabled
        ? plugin.contributions
            .filter((c) => c.location === "sidebar")
            .map((c) => ({
              href: pluginPageURL(plugin.id, c.id),
              label: c.label_zh || c.label,
              after: c.after,
              plugin: plugin.id,
              contribution: c.id,
            }))
        : [],
    ),
  );

  return (
    <nav className="flex flex-1 flex-col gap-1 p-2">
      {items.map((item) => {
        const Icon = item.icon;
        const active = isActive(pathname, search, item);
        return (
          <Link
            key={item.href}
            href={item.href}
            className={cn(
              "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm transition-colors",
              active
                ? "bg-sidebar-accent text-sidebar-accent-foreground font-medium"
                : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
            )}
          >
            <Icon className="size-4 shrink-0" />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}

function isActive(
  pathname: string,
  search: { get(name: string): string | null },
  item: { href: string; plugin?: string; contribution?: string },
) {
  if (item.plugin) {
    return (
      pathname.startsWith("/plugins") &&
      search.get("plugin") === item.plugin &&
      search.get("contribution") === item.contribution
    );
  }
  return item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
}

function buildNavItems(
  extras: {
    href: string;
    label: string;
    after?: string;
    plugin: string;
    contribution: string;
  }[],
) {
  const items: {
    href: string;
    label: string;
    icon: typeof Puzzle;
    plugin?: string;
    contribution?: string;
  }[] = NAV.map((n) => ({ href: n.href, label: n.label, icon: n.icon }));

  for (const extra of extras) {
    const entry = {
      href: extra.href,
      label: extra.label,
      icon: Puzzle,
      plugin: extra.plugin,
      contribution: extra.contribution,
    };
    const afterKey = (extra.after || "").replace(/^\//, "");
    const idx = items.findIndex((item) => {
      const key = item.href === "/" ? "dashboard" : item.href.replace(/^\//, "");
      return key === afterKey || item.href === extra.after;
    });
    if (idx >= 0) {
      items.splice(idx + 1, 0, entry);
    } else {
      const settingsAt = items.findIndex((item) => item.href === "/settings");
      if (settingsAt >= 0) items.splice(settingsAt, 0, entry);
      else items.push(entry);
    }
  }
  return items;
}
