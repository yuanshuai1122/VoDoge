"use client";

import Link from "next/link";
import { buttonVariants } from "@/components/ui/button";
import { useT } from "@/lib/i18n";

export default function NotFound() {
  const t = useT();
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 text-center">
      <div>
        <h2 className="text-lg font-semibold">{t("notFound.title")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t("notFound.body")}</p>
      </div>

      <Link href="/" className={buttonVariants({ size: "sm" })}>
        {t("notFound.back")}
      </Link>
    </div>
  );
}
