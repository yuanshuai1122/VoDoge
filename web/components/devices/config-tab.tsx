"use client";

import { useQuery } from "@tanstack/react-query";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { ErrorState } from "@/components/common/empty-state";
import { getDeviceConfig } from "@/lib/api/endpoints/devices";

/**
 * 设备配置。
 *
 * 当前为只读展示：后端的 PUT /devices/:id 接受的字段集合尚未在
 * frontend-api-matrix 中完整核对（矩阵 §9 标注了这一点），
 * 在核对清楚之前提供可编辑表单会有写坏配置的风险。
 */
export function ConfigTab({ deviceId }: { deviceId: string }) {
  const query = useQuery({
    queryKey: ["devices", "config", deviceId],
    queryFn: () => getDeviceConfig(deviceId),
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }

  if (query.isPending) return <Skeleton className="h-64" />;

  return (
    <div className="flex flex-col gap-3">
      <Alert>
        <AlertDescription>
          当前为只读视图。可编辑表单待核对 PUT 接口的完整字段后再提供，
          以免误改设备配置。
        </AlertDescription>
      </Alert>

      <pre className="overflow-x-auto rounded-lg border bg-muted/20 p-3 text-xs">
        {JSON.stringify(query.data, null, 2)}
      </pre>
    </div>
  );
}
