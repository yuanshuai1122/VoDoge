"use client";

import { useTheme } from "next-themes";
import { Moon, Sun, LogOut, AlertTriangle, Eye, EyeOff, Languages } from "lucide-react";
import { Button } from "@/components/ui/button";
import { logout } from "@/lib/api/endpoints/auth";
import { isExpiringWithin } from "@/lib/auth/token";
import { useHydrated } from "@/hooks/use-token";
import {
  useRevealSensitive,
  toggleRevealSensitive,
} from "@/components/common/sensitive";
import { setLocale, useLocale, useT } from "@/lib/i18n";

export function Header() {
  const { resolvedTheme, setTheme } = useTheme();
  const hydrated = useHydrated();
  const reveal = useRevealSensitive();
  const t = useT();
  const locale = useLocale();
  const expiringSoon = hydrated && isExpiringWithin(3);

  return (
    <header className="flex h-14 shrink-0 items-center justify-between gap-4 border-b px-4">
      <div className="flex items-center gap-2 md:hidden">
        <span className="font-semibold">{t("app.name")}</span>
      </div>

      <div className="flex flex-1 items-center justify-end gap-2">
        {expiringSoon && (
          <span className="flex items-center gap-1.5 text-xs text-amber-600 dark:text-amber-500">
            <AlertTriangle className="size-3.5" />
            {t("header.sessionExpiring")}
          </span>
        )}

        <Button
          variant="ghost"
          size="icon"
          aria-label={reveal ? t("header.hideSensitive") : t("header.showSensitive")}
          title={reveal ? t("header.hideIds") : t("header.showIds")}
          onClick={toggleRevealSensitive}
        >
          {reveal ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
        </Button>

        <Button
          variant="ghost"
          size="icon"
          aria-label={t("header.language")}
          title={locale === "zh" ? t("header.langEn") : t("header.langZh")}
          onClick={() => setLocale(locale === "zh" ? "en" : "zh")}
        >
          <Languages className="size-4" />
        </Button>

        <Button
          variant="ghost"
          size="icon"
          aria-label={t("header.toggleTheme")}
          onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
        >
          {hydrated && resolvedTheme === "dark" ? (
            <Sun className="size-4" />
          ) : (
            <Moon className="size-4" />
          )}
        </Button>

        <Button
          variant="ghost"
          size="icon"
          aria-label={t("header.logout")}
          onClick={() => void logout()}
        >
          <LogOut className="size-4" />
        </Button>
      </div>
    </header>
  );
}
