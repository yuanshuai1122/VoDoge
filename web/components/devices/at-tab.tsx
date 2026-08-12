"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { Send } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { executeAT } from "@/lib/api/endpoints/devices";
import { ApiError } from "@/lib/api/errors";

interface Exchange {
  cmd: string;
  response?: string;
  error?: string;
  at: Date;
}

/** 常用只读命令，避免用户手输出错。刻意不放会改状态的命令。 */
const TEMPLATES = [
  { label: "型号", cmd: "ATI" },
  { label: "信号", cmd: "AT+CSQ" },
  { label: "注册状态", cmd: "AT+CREG?" },
  { label: "运营商", cmd: "AT+COPS?" },
  { label: "IMEI", cmd: "AT+CGSN" },
  { label: "ICCID", cmd: "AT+QCCID" },
];

export function AtTab({ deviceId }: { deviceId: string }) {
  const [cmd, setCmd] = useState("");
  const [history, setHistory] = useState<Exchange[]>([]);

  const run = useMutation({
    mutationFn: (command: string) => executeAT(deviceId, command),
    onSuccess: (response, command) => {
      setHistory((h) => [{ cmd: command, response, at: new Date() }, ...h]);
      setCmd("");
    },
    onError: (e, command) => {
      const message = e instanceof ApiError ? e.message : "执行失败";
      setHistory((h) => [{ cmd: command, error: message, at: new Date() }, ...h]);
      toast.error(message);
    },
  });

  function submit() {
    const trimmed = cmd.trim();
    if (!trimmed) return;
    run.mutate(trimmed);
  }

  return (
    <div className="flex flex-col gap-4">
      <Alert>
        <AlertDescription>
          AT 命令直接下发到模组，错误的命令可能中断连接或改变配置。请确认后再执行。
        </AlertDescription>
      </Alert>

      <div className="flex flex-wrap gap-2">
        {TEMPLATES.map((t) => (
          <Button
            key={t.cmd}
            variant="outline"
            size="xs"
            disabled={run.isPending}
            onClick={() => setCmd(t.cmd)}
          >
            {t.label}
          </Button>
        ))}
      </div>

      <div className="flex gap-2">
        <Input
          value={cmd}
          placeholder="输入 AT 命令，例如 AT+CSQ"
          className="font-mono"
          disabled={run.isPending}
          onChange={(e) => setCmd(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        <Button onClick={submit} disabled={run.isPending || !cmd.trim()}>
          <Send className="size-4" />
          {run.isPending ? "执行中…" : "发送"}
        </Button>
      </div>

      {history.length === 0 ? (
        <p className="text-sm text-muted-foreground">尚无执行记录。</p>
      ) : (
        <div className="flex flex-col gap-3">
          {history.map((h, i) => (
            <div key={i} className="rounded-lg border p-3">
              <div className="flex items-center justify-between gap-2">
                <code className="text-xs font-medium">{h.cmd}</code>
                <span className="text-[11px] text-muted-foreground">
                  {h.at.toLocaleTimeString("zh-CN", { hour12: false })}
                </span>
              </div>
              <pre
                className={`mt-2 overflow-x-auto whitespace-pre-wrap break-all text-xs ${
                  h.error ? "text-destructive" : "text-muted-foreground"
                }`}
              >
                {h.error ?? h.response ?? ""}
              </pre>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
