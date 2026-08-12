"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { Send, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  executeUSSD,
  continueUSSD,
  cancelUSSD,
  type USSDResult,
} from "@/lib/api/endpoints/devices";
import { ApiError } from "@/lib/api/errors";

/**
 * USSD 是多轮会话：execute -> (continue)* -> cancel。
 * 续轮必须回传首轮返回的 session_id，因此会话态要在组件里维护。
 */
interface Turn {
  direction: "sent" | "received";
  text: string;
  channel?: string;
}

function resultText(r: USSDResult): string {
  const raw = r.result;
  if (typeof raw?.text === "string") return raw.text;
  // 不同后端（vowifi / cs）返回字段可能不同，兜底展示原始内容
  return typeof raw === "string" ? raw : JSON.stringify(raw ?? {}, null, 2);
}

export function UssdTab({ deviceId }: { deviceId: string }) {
  const [command, setCommand] = useState("");
  const [input, setInput] = useState("");
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [channel, setChannel] = useState<string | null>(null);
  const [turns, setTurns] = useState<Turn[]>([]);

  function applyResult(r: USSDResult) {
    const sid = r.result?.session_id;
    // 会话结束时后端不再返回 session_id
    setSessionId(typeof sid === "string" && sid ? sid : null);
    setChannel(r.channel ?? null);
    setTurns((t) => [
      ...t,
      { direction: "received", text: resultText(r), channel: r.channel },
    ]);
  }

  const onError = (e: unknown) =>
    toast.error(e instanceof ApiError ? e.message : "USSD 执行失败");

  const start = useMutation({
    mutationFn: (cmd: string) => executeUSSD(deviceId, cmd),
    onSuccess: (r, cmd) => {
      setTurns([{ direction: "sent", text: cmd }]);
      applyResult(r);
      setCommand("");
    },
    onError,
  });

  const next = useMutation({
    mutationFn: (value: string) => continueUSSD(deviceId, sessionId!, value),
    onSuccess: (r, value) => {
      setTurns((t) => [...t, { direction: "sent", text: value }]);
      applyResult(r);
      setInput("");
    },
    onError,
  });

  const stop = useMutation({
    mutationFn: () => cancelUSSD(deviceId, sessionId ?? undefined),
    onSuccess: () => {
      setSessionId(null);
      toast.success("会话已取消");
    },
    onError,
  });

  const busy = start.isPending || next.isPending || stop.isPending;
  const inSession = Boolean(sessionId);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        {inSession ? (
          <>
            <Badge variant="secondary">会话进行中</Badge>
            {channel && <Badge variant="outline">{channel}</Badge>}
            <Button
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => stop.mutate()}
            >
              <X className="size-4" />
              取消会话
            </Button>
          </>
        ) : (
          <Badge variant="outline">无进行中的会话</Badge>
        )}
      </div>

      {!inSession && (
        <div className="flex gap-2">
          <Input
            value={command}
            placeholder="输入 USSD 指令，例如 *100#"
            className="font-mono"
            disabled={busy}
            onChange={(e) => setCommand(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && command.trim()) start.mutate(command.trim());
            }}
          />
          <Button
            disabled={busy || !command.trim()}
            onClick={() => start.mutate(command.trim())}
          >
            <Send className="size-4" />
            {start.isPending ? "发送中…" : "发送"}
          </Button>
        </div>
      )}

      {turns.length > 0 && (
        <div className="flex flex-col gap-2 rounded-lg border p-3">
          {turns.map((t, i) => (
            <div
              key={i}
              className={
                t.direction === "sent"
                  ? "self-end rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground"
                  : "self-start whitespace-pre-wrap rounded-lg bg-muted px-3 py-2 text-sm"
              }
            >
              {t.text}
            </div>
          ))}
        </div>
      )}

      {inSession && (
        <div className="flex gap-2">
          <Input
            value={input}
            placeholder="输入本轮选项后回复"
            disabled={busy}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && input.trim()) next.mutate(input.trim());
            }}
          />
          <Button
            disabled={busy || !input.trim()}
            onClick={() => next.mutate(input.trim())}
          >
            <Send className="size-4" />
            {next.isPending ? "发送中…" : "回复"}
          </Button>
        </div>
      )}
    </div>
  );
}
