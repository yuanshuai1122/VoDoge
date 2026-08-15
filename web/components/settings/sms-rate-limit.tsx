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
import { getSMSSettings, updateSMSSettings } from "@/lib/api/endpoints/system";
import { ApiError } from "@/lib/api/errors";

const MAX = 200;

export function SMSRateLimitCard() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["settings", "sms"],
    queryFn: getSMSSettings,
  });
  const [limit, setLimit] = useState("");

  useEffect(() => {
    if (query.data) setLimit(String(query.data.hourly_limit));
  }, [query.data]);

  const parsed = Number.parseInt(limit, 10);
  const valid = Number.isInteger(parsed) && parsed >= 0 && parsed <= MAX;

  const save = useMutation({
    mutationFn: () => updateSMSSettings({ hourly_limit: parsed }),
    onSuccess: (data) => {
      toast.success(
        data.unlimited
          ? "已取消发送限额"
          : `每小时最多发送 ${data.hourly_limit} 条`,
      );
      queryClient.invalidateQueries({ queryKey: ["settings", "sms"] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">短信发送限额</CardTitle>
        <CardDescription>
          所有设备共用一个滚动 1 小时窗口。接收不限。0 表示不限制发送。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {query.isPending ? (
          <Skeleton className="h-20" />
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              {query.data?.unlimited
                ? `近一小时已发送 ${query.data.used} 条，当前不限制。`
                : `近一小时已发送 ${query.data?.used ?? 0} / ${query.data?.hourly_limit ?? 0} 条，剩余 ${query.data?.remaining ?? 0} 条。`}
            </p>
            <div className="flex flex-col gap-2">
              <Label htmlFor="sms_hourly_limit">每小时上限（0–{MAX}）</Label>
              <Input
                id="sms_hourly_limit"
                type="number"
                inputMode="numeric"
                min={0}
                max={MAX}
                value={limit}
                onChange={(e) => setLimit(e.target.value)}
              />
              {!valid && limit !== "" && (
                <p className="text-xs text-destructive">
                  请输入 0 到 {MAX} 的整数
                </p>
              )}
            </div>
            <Button
              type="button"
              disabled={!valid || save.isPending}
              onClick={() => save.mutate()}
              className="self-start"
            >
              {save.isPending ? "保存中…" : "保存限额"}
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  );
}
