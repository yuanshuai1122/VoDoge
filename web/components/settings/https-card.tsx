"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ErrorState } from "@/components/common/empty-state";
import {
  downloadHTTPSCertificate,
  getHTTPSSettings,
  updateHTTPSSettings,
} from "@/lib/api/endpoints/system";
import { ApiError } from "@/lib/api/errors";

export function HTTPSCard() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["settings", "https"],
    queryFn: getHTTPSSettings,
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["settings", "https"] });

  const toggle = useMutation({
    mutationFn: (enabled: boolean) => updateHTTPSSettings({ enabled }),
    onSuccess: (data) => {
      toast.success(data.enabled ? "已开启本机 HTTPS" : "已关闭，恢复 HTTP");
      invalidate();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  const download = useMutation({
    mutationFn: downloadHTTPSCertificate,
    onSuccess: () => toast.success("已开始下载证书"),
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "下载失败"),
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }

  const enabled = !!query.data?.enabled;

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
        <div className="space-y-1.5">
          <CardTitle className="text-base">本机自签 HTTPS</CardTitle>
          <CardDescription>
            与 HTTP 共用同一端口。开启后明文访问会跳到 HTTPS，便于把网页装到手机主屏幕。
          </CardDescription>
        </div>
        {query.isPending ? (
          <Skeleton className="h-5 w-10" />
        ) : (
          <Switch
            checked={enabled}
            disabled={toggle.isPending}
            onCheckedChange={(v) => toggle.mutate(v)}
            aria-label="开关本机 HTTPS"
          />
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {query.isPending ? (
          <Skeleton className="h-20" />
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              {enabled
                ? "已强制 HTTPS。HTTP 请求会跳转。关闭后立即回到 HTTP。"
                : "当前是 HTTP。建议先下载并信任证书，再打开开关。"}
            </p>
            {query.data?.fingerprint ? (
              <div className="rounded-md border bg-muted/40 p-3">
                <p className="text-xs text-muted-foreground">SHA-256</p>
                <p className="break-all font-mono text-xs">
                  {query.data.fingerprint}
                </p>
              </div>
            ) : null}
            <p className="text-xs text-amber-700 dark:text-amber-400">
              自签证书需要在系统或浏览器里信任，否则会一直提示连接不安全。
            </p>
            <Button
              type="button"
              variant="outline"
              className="self-start"
              disabled={download.isPending}
              onClick={() => download.mutate()}
            >
              {download.isPending ? "下载中…" : "下载自签证书"}
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  );
}
