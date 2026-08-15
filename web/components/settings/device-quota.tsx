"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ErrorState } from "@/components/common/empty-state";
import { getDeviceQuota, updateDeviceQuota } from "@/lib/api/endpoints/system";
import { ApiError } from "@/lib/api/errors";

export function DeviceQuotaCard() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["settings", "devices"],
    queryFn: getDeviceQuota,
  });
  const [limit, setLimit] = useState("");

  useEffect(() => {
    if (query.data) setLimit(String(query.data.limit));
  }, [query.data]);

  const max = query.data?.max_limit ?? 10;
  const parsed = Number.parseInt(limit, 10);
  const valid = Number.isInteger(parsed) && parsed >= 1 && parsed <= max;

  const save = useMutation({
    mutationFn: () => updateDeviceQuota({ limit: parsed }),
    onSuccess: (data) => {
      toast.success(`设备配额已设为 ${data.limit} 台`);
      queryClient.invalidateQueries({ queryKey: ["settings", "devices"] });
      queryClient.invalidateQueries({ queryKey: ["devices"] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">设备配额</CardTitle>
        <CardDescription>
          最多允许配置的模组 / 读卡器数量。调低不会删除已经添加的设备。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {query.isPending ? (
          <Skeleton className="h-20" />
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              已配置 {query.data?.used ?? 0} / {query.data?.limit ?? 0} 台。默认{" "}
              {query.data?.default_limit ?? 5} 台，上限 {max} 台。
            </p>
            <div className="flex flex-col gap-2">
              <Label htmlFor="device_quota_limit">最多设备数（1–{max}）</Label>
              <Input
                id="device_quota_limit"
                type="number"
                inputMode="numeric"
                min={1}
                max={max}
                value={limit}
                onChange={(e) => setLimit(e.target.value)}
              />
              {!valid && limit !== "" && (
                <p className="text-xs text-destructive">
                  请输入 1 到 {max} 的整数
                </p>
              )}
            </div>
            <Button
              type="button"
              disabled={!valid || save.isPending}
              onClick={() => save.mutate()}
              className="self-start"
            >
              {save.isPending ? "保存中…" : "保存配额"}
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  );
}
