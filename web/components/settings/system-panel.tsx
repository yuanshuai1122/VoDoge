"use client";

import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState } from "@/components/common/empty-state";
import {
  getSystemInfo,
  checkUpdate,
  applyUpdate,
} from "@/lib/api/endpoints/system";
import { ApiError } from "@/lib/api/errors";

export function SystemPanel() {
  const info = useQuery({
    queryKey: ["system", "info"],
    queryFn: getSystemInfo,
  });

  const [updateInfo, setUpdateInfo] = useState<unknown>(null);

  const check = useMutation({
    mutationFn: checkUpdate,
    onSuccess: (r) => {
      setUpdateInfo(r);
      toast.success("已检查更新");
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "检查失败"),
  });

  const apply = useMutation({
    mutationFn: applyUpdate,
    onSuccess: () => {
      // 应用更新会替换二进制并重启服务，当前连接大概率中断
      toast.success("已触发更新，服务可能重启，请稍后刷新页面");
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "更新失败"),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">系统信息</CardTitle>
      </CardHeader>

      <CardContent className="flex flex-col gap-4">
        {info.isError ? (
          <ErrorState error={info.error} onRetry={() => info.refetch()} />
        ) : info.isPending ? (
          <Skeleton className="h-24" />
        ) : (
          <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2">
            {Object.entries(info.data ?? {}).map(([k, v]) => (
              <div key={k} className="flex justify-between gap-4">
                <dt className="text-sm text-muted-foreground">{k}</dt>
                <dd className="truncate text-sm">
                  {v === null || v === undefined
                    ? "-"
                    : typeof v === "object"
                      ? JSON.stringify(v)
                      : String(v)}
                </dd>
              </div>
            ))}
          </dl>
        )}

        <div className="flex flex-wrap items-center gap-2 border-t pt-4">
          <Button
            variant="outline"
            size="sm"
            disabled={check.isPending}
            onClick={() => check.mutate()}
          >
            {check.isPending ? "检查中…" : "检查更新"}
          </Button>

          <Button
            variant="outline"
            size="sm"
            disabled={apply.isPending || !updateInfo}
            onClick={() => {
              if (
                confirm(
                  "应用更新会替换程序并重启服务，期间设备连接会中断。确定继续？",
                )
              ) {
                apply.mutate();
              }
            }}
          >
            {apply.isPending ? "更新中…" : "应用更新"}
          </Button>
        </div>

        {updateInfo != null && (
          <Alert>
            <AlertDescription>
              <pre className="overflow-x-auto text-xs">
                {JSON.stringify(updateInfo, null, 2)}
              </pre>
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}
