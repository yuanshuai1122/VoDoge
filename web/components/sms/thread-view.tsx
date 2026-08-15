"use client";

import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowLeft, Send, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import {
  listThread,
  nextThreadCursor,
  sendSMS,
  deleteThread,
  SMS_PAGE_SIZE,
  type ThreadCursor,
} from "@/lib/api/endpoints/sms";
import { isInbound, isSendFailed, type SMSMessage } from "@/types/sms";
import { ApiError } from "@/lib/api/errors";
import { formatDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { ContactKey } from "./contact-list";
import { DeliveryStatus } from "./delivery-status";
import { useT } from "@/lib/i18n";

export function ThreadView({
  contact,
  onBack,
}: {
  contact: ContactKey;
  onBack?: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState("");
  // 仅 VoWiFi 通道会返回 message_id，用它追踪刚发出那条的回执
  const [trackedMessageId, setTrackedMessageId] = useState<string | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  const threadKey = ["sms", "thread", contact.imsi, contact.peer] as const;

  const query = useInfiniteQuery({
    queryKey: threadKey,
    queryFn: ({ pageParam }) =>
      listThread({
        peer: contact.peer,
        imsi: contact.imsi || undefined,
        device_id: contact.device_id || undefined,
        cursor: pageParam,
      }),
    initialPageParam: undefined as ThreadCursor | undefined,
    getNextPageParam: (lastPage) => nextThreadCursor(lastPage, SMS_PAGE_SIZE),
    refetchInterval: 10_000,
  });

  const send = useMutation({
    mutationFn: (message: string) =>
      sendSMS({
        phone: contact.peer,
        message,
        device_id: contact.device_id || undefined,
        imsi: contact.imsi || undefined,
      }),
    onSuccess: (result) => {
      setDraft("");
      // 长短信会被拆成多条分片，按条计费，值得告知
      const parts =
        result?.parts_total > 1 ? t("sms.parts", { n: result.parts_total }) : "";
      const via =
        result?.transport === "ims"
          ? t("sms.viaIms")
          : result?.transport === "cellular"
            ? t("sms.viaCellular")
            : "";
      toast.success(`${t("sms.sent")}${via}${parts}`);
      setTrackedMessageId(result?.message_id || null);
      queryClient.invalidateQueries({ queryKey: threadKey });
      queryClient.invalidateQueries({ queryKey: ["sms", "contacts"] });
    },
    onError: (e) => {
      if (e instanceof ApiError && e.isRateLimited) {
        toast.error(e.message || t("sms.rateLimited"));
        return;
      }
      toast.error(e instanceof ApiError ? e.message : t("sms.sendFailed"));
    },
  });

  const removeThread = useMutation({
    mutationFn: () =>
      deleteThread({
        peer: contact.peer,
        imsi: contact.imsi || undefined,
        device_id: contact.device_id || undefined,
      }),
    onSuccess: () => {
      toast.success(t("sms.threadDeleted"));
      queryClient.invalidateQueries({ queryKey: ["sms", "contacts"] });
      queryClient.removeQueries({ queryKey: threadKey });
      onBack?.();
    },
    onError: (e) => {
      toast.error(e instanceof ApiError ? e.message : t("sms.deleteFailed"));
    },
  });

  // 游标向过去翻页，接口返回按时间倒序；聊天界面需要正序
  const messages: SMSMessage[] = query.data
    ? [...query.data.pages.flat()].reverse()
    : [];

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [contact.peer, query.data?.pages.length]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center justify-between gap-2 border-b px-3 py-2.5 md:px-4">
        <div className="flex min-w-0 items-center gap-1">
          {onBack && (
            <Button
              variant="ghost"
              size="icon"
              className="md:hidden"
              aria-label={t("sms.back")}
              onClick={onBack}
            >
              <ArrowLeft className="size-4" />
            </Button>
          )}
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">{contact.peer}</p>
            <p className="truncate text-xs text-muted-foreground">
              {contact.device_name || t("sms.deviceOffline")}
              {contact.local_phone && ` · ${t("sms.localPhone", { phone: contact.local_phone })}`}
            </p>
          </div>
        </div>

        <Button
          variant="ghost"
          size="icon"
          aria-label={t("sms.deleteThread")}
          disabled={removeThread.isPending}
          onClick={() => {
            if (confirm(t("sms.deleteConfirm", { peer: contact.peer }))) {
              removeThread.mutate();
            }
          }}
        >
          <Trash2 className="size-4" />
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-4">
        {query.isError ? (
          <ErrorState error={query.error} onRetry={() => query.refetch()} />
        ) : query.isPending ? (
          <div className="flex flex-col gap-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-14" />
            ))}
          </div>
        ) : messages.length === 0 ? (
          <EmptyState title={t("sms.emptyThread")} description={t("sms.emptyThreadHint")} />
        ) : (
          <>
            {query.hasNextPage && (
              <div className="mb-3 flex justify-center">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={query.isFetchingNextPage}
                  onClick={() => query.fetchNextPage()}
                >
                  {query.isFetchingNextPage ? t("sms.loading") : t("sms.loadEarlier")}
                </Button>
              </div>
            )}

            <div className="flex flex-col gap-3">
              {messages.map((m) => (
                <MessageBubble key={m.id} message={m} />
              ))}
            </div>
          </>
        )}
        <div ref={bottomRef} />
      </div>

      <div className="shrink-0 border-t p-3">
        {trackedMessageId && (
          <div className="mb-2">
            <DeliveryStatus
              key={trackedMessageId}
              messageId={trackedMessageId}
              onDismiss={() => setTrackedMessageId(null)}
            />
          </div>
        )}
        <div className="flex items-end gap-2">
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={t("sms.to", { peer: contact.peer })}
            rows={2}
            className="min-h-0 resize-none"
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && draft.trim()) {
                send.mutate(draft.trim());
              }
            }}
          />
          <Button
            size="icon"
            aria-label={t("sms.send")}
            disabled={!draft.trim() || send.isPending}
            onClick={() => send.mutate(draft.trim())}
          >
            <Send className="size-4" />
          </Button>
        </div>
        <p className="mt-1.5 text-[11px] text-muted-foreground">
          Ctrl/Cmd + Enter 发送
        </p>
      </div>
    </div>
  );
}

function MessageBubble({ message }: { message: SMSMessage }) {
  const inbound = isInbound(message);
  const failed = isSendFailed(message);

  return (
    <div className={cn("flex", inbound ? "justify-start" : "justify-end")}>
      <div className="flex max-w-[75%] flex-col gap-1">
        <div
          className={cn(
            "rounded-lg px-3 py-2 text-sm whitespace-pre-wrap break-words",
            inbound ? "bg-muted" : "bg-primary text-primary-foreground",
            failed && "border border-destructive",
          )}
        >
          {message.content}
        </div>
        <span
          className={cn(
            "text-[11px] text-muted-foreground",
            inbound ? "text-left" : "text-right",
          )}
        >
          {formatDateTime(message.timestamp)}
          {failed && <span className="ml-1 text-destructive">发送失败</span>}
        </span>
      </div>
    </div>
  );
}
