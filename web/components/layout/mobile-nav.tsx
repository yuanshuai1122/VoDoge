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
import { useT, type MessageKey } from "@/lib/i18n";

const NAV: { href: string; labelKey: MessageKey; icon: typeof LayoutDashboard }[] = [
  { href: "/", labelKey: "nav.home", icon: LayoutDashboard },
  { href: "/devices", labelKey: "nav.devicesShort", icon: Smartphone },
  { href: "/sms", labelKey: "nav.smsShort", icon: MessageSquare },
  { href: "/proxy", labelKey: "nav.proxyShort", icon: Network },
  { href: "/logs", labelKey: "nav.logsShort", icon: ScrollText },
  { href: "/settings", labelKey: "nav.settingsShort", icon: Settings },
];

export function MobileNav() {
  const pathname = usePathname();
  const t = useT();

  return (
    <nav
      aria-label={t("nav.main")}
      className="fixed inset-x-0 bottom-0 z-40 border-t bg-background/95 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden"
    >
      <ul className="grid grid-cols-6">
        {NAV.map(({ href, labelKey, icon: Icon }) => {
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
                {t(labelKey)}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
