"use client";

import { useQuery } from "@tanstack/react-query";
import { RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState } from "@/components/common/empty-state";
import { Sensitive } from "@/components/common/sensitive";
import {
  listNotifications,
  retryNotification,
  getChipInfo,
} from "@/lib/api/endpoints/esim";
import { useEsimMutation } from "@/hooks/use-esim-mutation";
import { useEsimLock } from "@/stores/esim-lock";

/**
 * eSIM 通知队列。
 *
 * 通知是 RSP 流程里必须回送给运营商 SM-DP+ 的回执（下载/启用/删除）。
 * 回送失败会滞留在 eUICC 上，可能导致运营商侧状态与本机不一致，因此需要能手动重试。
 */
export function EsimNotifications({ deviceId }: { deviceId: string }) {
  const lock = useEsimLock(deviceId);

  const query = useQuery({
    queryKey: ["esim", deviceId, "notifications"],
    queryFn: () => listNotifications(deviceId),
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  });

  const retry = useEsimMutation({
    deviceId,
    operation: "retry_notification",
    mutationFn: (sequence: number) => retryNotification(deviceId, sequence),
    successMessage: "已重新回送",
  });

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-4">
        <CardTitle className="text-base">通知队列</CardTitle>
        <Button
          variant="outline"
          size="sm"
          disabled={lock.locked || query.isFetching}
          onClick={() => query.refetch()}
        >
          刷新
        </Button>
      </CardHeader>

      <CardContent>
        {query.isError ? (
          <ErrorState error={query.error} onRetry={() => query.refetch()} />
        ) : query.isPending ? (
          <Skeleton className="h-24" />
        ) : query.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            没有待回送的通知。
          </p>
        ) : (
          <div className="flex flex-col divide-y">
            {query.data.map((n) => (
              <div
                key={n.sequence_number}
                className="flex flex-wrap items-center justify-between gap-3 py-2.5"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{n.event}</span>
                    <Badge variant="outline">#{n.sequence_number}</Badge>
                    {!n.can_retry && (
                      <Badge variant="secondary">不可重试</Badge>
                    )}
                  </div>
                  <p className="mt-0.5 font-mono text-xs text-muted-foreground">
                    {n.iccid && <Sensitive value={n.iccid} />}
                    {n.address && ` · ${n.address}`}
                  </p>
                </div>

                <Button
                  variant="ghost"
                  size="sm"
                  disabled={lock.locked || !n.can_retry}
                  onClick={() => retry.mutate(n.sequence_number)}
                >
                  <RotateCw className="size-4" />
                  重试
                </Button>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

/** eUICC 芯片信息（裸对象，字段随芯片厂商变化，按键值对展示）。 */
export function EsimChipInfo({ deviceId }: { deviceId: string }) {
  const query = useQuery({
    queryKey: ["esim", deviceId, "chip-info"],
    queryFn: () => getChipInfo(deviceId),
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
  });

  if (query.isError) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base">芯片信息</CardTitle>
        </CardHeader>
        <CardContent>
          <ErrorState error={query.error} onRetry={() => query.refetch()} />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">芯片信息</CardTitle>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <Skeleton className="h-24" />
        ) : (
          <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2">
            {Object.entries(query.data ?? {}).map(([k, v]) => (
              <div key={k} className="flex justify-between gap-4">
                <dt className="text-sm text-muted-foreground">{k}</dt>
                <dd className="truncate font-mono text-xs">
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
      </CardContent>
    </Card>
  );
}
