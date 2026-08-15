"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useEventSource, type SSEStatus } from "@/lib/sse/use-event-source";
import { cn } from "@/lib/utils";
import { LOG_LEVELS, type LogEntry } from "@/types/log";
import { useT } from "@/lib/i18n";

/** 只保留最近 N 条，避免长时间挂着把内存吃满。 */
const MAX_ENTRIES = 2000;

export default function LogsPage() {
  const t = useT();
  const [level, setLevel] = useState<string>("info");
  const [autoScroll, setAutoScroll] = useState(true);
  // 切换等级或点清空时用 key 重建流组件，比在 effect 里重置 state 更直接
  const [clearNonce, setClearNonce] = useState(0);
  const [status, setStatus] = useState<SSEStatus>("connecting");

  return (
    <>
      <PageHeader
        title={t("logs.title")}
        description={t("logs.desc")}
        actions={
          <div className="flex items-center gap-2">
            <StatusBadge status={status} />
            <Select value={level} onValueChange={(v) => v && setLevel(v)}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {LOG_LEVELS.map((l) => (
                  <SelectItem key={l} value={l}>
                    {l}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              variant={autoScroll ? "secondary" : "outline"}
              size="sm"
              onClick={() => setAutoScroll((v) => !v)}
            >
              {autoScroll ? t("logs.follow") : t("logs.paused")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setClearNonce((n) => n + 1)}
            >
              {t("logs.clear")}
            </Button>
          </div>
        }
      />

      <LogStream
        key={`${level}:${clearNonce}`}
        level={level}
        autoScroll={autoScroll}
        onStatusChange={setStatus}
      />
    </>
  );
}

function LogStream({
  level,
  autoScroll,
  onStatusChange,
}: {
  level: string;
  autoScroll: boolean;
  onStatusChange: (s: SSEStatus) => void;
}) {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);

  const onLog = useCallback((data: unknown) => {
    const entry = data as LogEntry;
    if (!entry || typeof entry.message !== "string") return;
    setEntries((prev) =>
      prev.length >= MAX_ENTRIES
        ? [...prev.slice(-MAX_ENTRIES + 1), entry]
        : [...prev, entry],
    );
  }, []);

  const status = useEventSource("/logs/stream", {
    events: { log: onLog },
    query: { level },
  });

  useEffect(() => {
    onStatusChange(status);
  }, [status, onStatusChange]);

  useEffect(() => {
    if (autoScroll) bottomRef.current?.scrollIntoView({ block: "end" });
  }, [entries, autoScroll]);

  return (
    <div className="h-[calc(100svh-13rem)] overflow-auto rounded-lg border bg-muted/20 p-3 font-mono text-xs">
      {entries.length === 0 ? (
        <p className="text-muted-foreground">
          {status === "open" ? "已连接，等待日志…" : "正在连接日志流…"}
        </p>
      ) : (
        entries.map((e, i) => (
          <div key={i} className="flex gap-2 py-0.5 leading-relaxed">
            <span className="shrink-0 text-muted-foreground">{e.time}</span>
            <span className={cn("w-12 shrink-0 uppercase", levelClass(e.level))}>
              {e.level}
            </span>
            <span className="min-w-0 break-all">
              {e.message}
              {e.fields && (
                <span className="ml-2 text-muted-foreground">{e.fields}</span>
              )}
            </span>
          </div>
        ))
      )}
      <div ref={bottomRef} />
    </div>
  );
}

function levelClass(level: string): string {
  switch (level.toLowerCase()) {
    case "error":
    case "fatal":
      return "text-destructive";
    case "warn":
      return "text-amber-600 dark:text-amber-500";
    case "debug":
      return "text-muted-foreground";
    default:
      return "text-foreground";
  }
}

function StatusBadge({ status }: { status: SSEStatus }) {
  const map: Record<
    SSEStatus,
    { label: string; variant: "default" | "secondary" | "destructive" }
  > = {
    open: { label: "已连接", variant: "default" },
    connecting: { label: "连接中", variant: "secondary" },
    closed: { label: "已断开", variant: "secondary" },
    error: { label: "连接失败", variant: "destructive" },
  };
  const { label, variant } = map[status];
  return <Badge variant={variant}>{label}</Badge>;
}
