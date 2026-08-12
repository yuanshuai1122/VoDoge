"use client";

import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useEventSource } from "@/lib/sse/use-event-source";
import { downloadProfileStreamPath } from "@/lib/api/endpoints/esim";
import { useEsimLockStore } from "@/stores/esim-lock";
import type { EsimDownloadEvent } from "@/types/esim";

interface DownloadParams {
  smdp: string;
  matching_id: string;
  confirmation_code: string;
}

export function EsimDownloadDialog({
  deviceId,
  open,
  onOpenChange,
  disabled,
}: {
  deviceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  disabled?: boolean;
}) {
  const queryClient = useQueryClient();
  const begin = useEsimLockStore((s) => s.begin);
  const end = useEsimLockStore((s) => s.end);

  const [form, setForm] = useState<DownloadParams>({
    smdp: "",
    matching_id: "",
    confirmation_code: "",
  });
  // 非空表示正在下载，同时作为 SSE 的 query 参数
  const [active, setActive] = useState<DownloadParams | null>(null);
  const [progress, setProgress] = useState<EsimDownloadEvent | null>(null);

  const finish = useCallback(
    (ok: boolean, event: EsimDownloadEvent) => {
      setActive(null);
      end(deviceId);
      if (ok) {
        toast.success(event.warning ? `下载完成（${event.warning}）` : "Profile 下载完成");
        queryClient.invalidateQueries({ queryKey: ["esim", deviceId] });
      } else {
        toast.error(event.msg || "下载失败");
      }
    },
    [deviceId, end, queryClient],
  );

  // 该流没有 event 名，走默认的 message
  const onMessage = useCallback(
    (data: unknown) => {
      const event = data as EsimDownloadEvent;
      if (!event || typeof event.step !== "string") return;

      setProgress(event);
      if (event.step === "done") finish(true, event);
      else if (event.step === "error") finish(false, event);
    },
    [finish],
  );

  const status = useEventSource(downloadProfileStreamPath(deviceId), {
    events: { message: onMessage },
    enabled: active !== null,
    query: active
      ? {
          smdp: active.smdp,
          matching_id: active.matching_id || undefined,
          confirmation_code: active.confirmation_code || undefined,
        }
      : undefined,
  });

  function start() {
    if (!form.smdp.trim()) {
      toast.error("SM-DP+ 地址为必填项");
      return;
    }
    setProgress(null);
    begin(deviceId, "download_profile");
    setActive({ ...form });
  }

  const downloading = active !== null;
  const pct = progress?.pct ?? 0;

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        // 下载中不允许关闭：关闭会断开 SSE，而后端仍在执行
        if (downloading) return;
        onOpenChange(v);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>下载 eSIM Profile</DialogTitle>
          <DialogDescription>
            填写运营商提供的 SM-DP+ 信息。下载最长约 5 分钟，期间请勿关闭。
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="smdp">SM-DP+ 地址</Label>
            <Input
              id="smdp"
              placeholder="例如 rsp.example.com"
              value={form.smdp}
              disabled={downloading}
              onChange={(e) => setForm((f) => ({ ...f, smdp: e.target.value }))}
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="matching_id">激活码 / Matching ID</Label>
            <Input
              id="matching_id"
              value={form.matching_id}
              disabled={downloading}
              onChange={(e) =>
                setForm((f) => ({ ...f, matching_id: e.target.value }))
              }
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="confirmation_code">确认码（可选）</Label>
            <Input
              id="confirmation_code"
              value={form.confirmation_code}
              disabled={downloading}
              onChange={(e) =>
                setForm((f) => ({ ...f, confirmation_code: e.target.value }))
              }
            />
          </div>

          {downloading && (
            <div className="flex flex-col gap-2">
              <div className="h-2 overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full bg-primary transition-all"
                  style={{ width: `${Math.max(0, Math.min(100, pct))}%` }}
                />
              </div>
              <p className="text-xs text-muted-foreground">
                {progress?.msg ??
                  (status === "open" ? "已连接，等待进度…" : "正在建立连接…")}
              </p>
            </div>
          )}

          {progress?.step === "error" && (
            <Alert variant="destructive">
              <AlertDescription>
                {progress.msg}
                {progress.code && (
                  <span className="mt-1 block font-mono text-xs">
                    错误码 {progress.code}
                  </span>
                )}
                {progress.details && (
                  <span className="mt-1 block text-xs">{progress.details}</span>
                )}
              </AlertDescription>
            </Alert>
          )}
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            disabled={downloading}
            onClick={() => onOpenChange(false)}
          >
            关闭
          </Button>
          <Button onClick={start} disabled={downloading || disabled}>
            {downloading ? "下载中…" : "开始下载"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
