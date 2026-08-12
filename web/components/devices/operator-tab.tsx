"use client";

import { useCallback, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Radar, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState } from "@/components/common/empty-state";
import { useEventSource } from "@/lib/sse/use-event-source";
import {
  getOperatorSelection,
  setOperatorSelection,
  operatorScanStreamPath,
  type OperatorCandidate,
  type OperatorScanResponse,
} from "@/lib/api/endpoints/operator";
import { ApiError } from "@/lib/api/errors";
import { cn } from "@/lib/utils";

/**
 * 运营商选择。
 *
 * 扫描是长耗时操作（模组需遍历频段），后端提供 SSE 流式返回中间结果，
 * 因此这里边扫边显示候选，而不是等一个最终响应。
 */
export function OperatorTab({ deviceId }: { deviceId: string }) {
  const queryClient = useQueryClient();
  const queryKey = ["devices", "operator", deviceId] as const;

  const [scanning, setScanning] = useState(false);
  const [scan, setScan] = useState<OperatorScanResponse | null>(null);

  const current = useQuery({
    queryKey,
    queryFn: () => getOperatorSelection(deviceId),
  });

  const onScanEvent = useCallback((data: unknown) => {
    const r = data as OperatorScanResponse;
    if (!r || typeof r.status !== "string") return;
    setScan(r);
    if (r.complete) {
      setScanning(false);
      if (r.error) toast.error(r.error);
      else toast.success(`扫描完成，发现 ${r.candidates?.length ?? 0} 个网络`);
    }
  }, []);

  const streamStatus = useEventSource(operatorScanStreamPath(deviceId), {
    events: { operator_scan: onScanEvent },
    enabled: scanning,
  });

  const select = useMutation({
    mutationFn: (input: Parameters<typeof setOperatorSelection>[1]) =>
      setOperatorSelection(deviceId, input),
    onSuccess: (sel) => {
      toast.success(
        sel.mode === "automatic" ? "已恢复自动选网" : `已锁定 ${sel.operator_name || sel.plmn}`,
      );
      queryClient.invalidateQueries({ queryKey });
      queryClient.invalidateQueries({ queryKey: ["devices"] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "设置失败"),
  });

  const busy = select.isPending;

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">当前选网</CardTitle>
        </CardHeader>
        <CardContent>
          {current.isError ? (
            <ErrorState error={current.error} onRetry={() => current.refetch()} />
          ) : current.isPending ? (
            <Skeleton className="h-16" />
          ) : (
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <Badge
                    variant={
                      current.data.mode === "automatic" ? "secondary" : "default"
                    }
                  >
                    {current.data.mode === "automatic" ? "自动" : "手动锁定"}
                  </Badge>
                  <span className="text-sm font-medium">
                    {current.data.operator_name || current.data.plmn || "-"}
                  </span>
                </div>
                {current.data.rat && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    制式 {current.data.rat}
                  </p>
                )}
              </div>

              {current.data.mode === "manual" && (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() => select.mutate({ mode: "automatic" })}
                >
                  恢复自动
                </Button>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center justify-between gap-4">
          <CardTitle className="text-base">扫描可用网络</CardTitle>
          <Button
            size="sm"
            disabled={scanning || busy}
            onClick={() => {
              setScan(null);
              setScanning(true);
            }}
          >
            <Radar className={cn("size-4", scanning && "animate-pulse")} />
            {scanning ? "扫描中…" : "开始扫描"}
          </Button>
        </CardHeader>

        <CardContent className="flex flex-col gap-3">
          <Alert>
            <AlertDescription>
              扫描期间模组会脱网，可能持续 1–2 分钟，其间该设备无法收发数据与短信。
            </AlertDescription>
          </Alert>

          {scanning && (
            <p className="text-sm text-muted-foreground">
              {scan?.message ||
                (streamStatus === "open" ? "已连接，等待结果…" : "正在建立连接…")}
            </p>
          )}

          {scan?.error && (
            <Alert variant="destructive">
              <AlertDescription>
                {scan.error}
                {scan.retryable && "（可重试）"}
              </AlertDescription>
            </Alert>
          )}

          {scan?.candidates && scan.candidates.length > 0 && (
            <div className="flex flex-col divide-y rounded-lg border">
              {scan.candidates.map((c) => (
                <CandidateRow
                  key={c.plmn}
                  candidate={c}
                  disabled={busy || scanning}
                  onSelect={() =>
                    select.mutate({
                      mode: "manual",
                      plmn: c.plmn,
                      mcc: c.mcc,
                      mnc: c.mnc,
                      mnc_length: c.mnc_length,
                      includes_pcs_digit: c.includes_pcs_digit,
                      rat: c.rats?.[0],
                    })
                  }
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function CandidateRow({
  candidate,
  disabled,
  onSelect,
}: {
  candidate: OperatorCandidate;
  disabled: boolean;
  onSelect: () => void;
}) {
  const forbidden = candidate.status?.toLowerCase() === "forbidden";
  const isCurrent = candidate.status?.toLowerCase() === "current";

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 px-3 py-2.5">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">
            {candidate.operator_name || candidate.short_name || candidate.plmn}
          </span>
          {isCurrent && (
            <Badge variant="default" className="gap-1">
              <Check className="size-3" />
              当前
            </Badge>
          )}
          {forbidden && <Badge variant="destructive">禁止</Badge>}
        </div>
        <p className="mt-0.5 font-mono text-xs text-muted-foreground">
          PLMN {candidate.plmn}
          {candidate.rats?.length ? ` · ${candidate.rats.join("/")}` : ""}
        </p>
      </div>

      <Button
        variant="outline"
        size="sm"
        // 禁止接入的网络锁上去只会一直搜不到网
        disabled={disabled || forbidden || isCurrent}
        onClick={onSelect}
      >
        锁定
      </Button>
    </div>
  );
}
