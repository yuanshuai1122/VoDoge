"use client";

import { useState } from "react";
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
import type { SMSSettings } from "@/types/sms";

const MAX = 200;

export function SMSRateLimitCard() {
  const query = useQuery({
    queryKey: ["settings", "sms"],
    queryFn: getSMSSettings,
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
        {query.isPending || !query.data ? (
          <Skeleton className="h-20" />
        ) : (
          <SMSRateLimitForm data={query.data} />
        )}
      </CardContent>
    </Card>
  );
}

function SMSRateLimitForm({ data }: { data: SMSSettings }) {
  const queryClient = useQueryClient();
  const [limit, setLimit] = useState(() => String(data.hourly_limit));
  const parsed = Number.parseInt(limit, 10);
  const valid = Number.isInteger(parsed) && parsed >= 0 && parsed <= MAX;

  const save = useMutation({
    mutationFn: () => updateSMSSettings({ hourly_limit: parsed }),
    onSuccess: (updated) => {
      toast.success(
        updated.unlimited
          ? "已取消发送限额"
          : `每小时最多发送 ${updated.hourly_limit} 条`,
      );
      queryClient.invalidateQueries({ queryKey: ["settings", "sms"] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  return (
    <>
      <p className="text-sm text-muted-foreground">
        {data.unlimited
          ? `近一小时已发送 ${data.used} 条，当前不限制。`
          : `近一小时已发送 ${data.used} / ${data.hourly_limit} 条，剩余 ${data.remaining} 条。`}
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
          <p className="text-xs text-destructive">请输入 0 到 {MAX} 的整数</p>
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
  );
}
