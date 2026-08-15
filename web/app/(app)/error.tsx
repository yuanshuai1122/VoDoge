"use client"; // 错误边界必须是客户端组件

import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";

/**
 * 应用区错误边界。
 *
 * 注意 Next 16 的回调参数名是 `retry`（旧版本为 `reset`），
 * 见 node_modules/next/dist/docs/01-app/03-api-reference/03-file-conventions/error.md。
 */
export default function AppError({
  error,
  retry,
}: {
  error: Error & { digest?: string };
  retry: () => void;
}) {
  const t = useT();
  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <div className="flex min-h-[60svh] flex-col items-center justify-center gap-4 text-center">
      <div>
        <h2 className="text-lg font-semibold">{t("error.title")}</h2>
        <p className="mt-1 max-w-md text-sm text-muted-foreground">
          {error.message || t("error.fallback")}
        </p>
        {error.digest && (
          <p className="mt-1 font-mono text-xs text-muted-foreground">
            digest: {error.digest}
          </p>
        )}
      </div>

      <Button size="sm" onClick={() => retry()}>
        {t("common.retry")}
      </Button>
    </div>
  );
}
