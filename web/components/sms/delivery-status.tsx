"use client";

import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { getDeliveryStatus } from "@/lib/api/endpoints/sms";
import { ApiError } from "@/lib/api/errors";
import { cn } from "@/lib/utils";

/**
 * 刚发出那条短信的投递追踪。
 *
 * 只有 VoWiFi 通道会生成 `message_id`——AT 通道发送拿不到，因此没有可查的记录，
 * 后端返回 404 属正常，这里不当作错误展示。
 *
 * 长短信会被拆成多条分片，逐条回执，所以进度是 `acks / parts_total`。
 */
export function DeliveryStatus({
  messageId,
  onDismiss,
}: {
  messageId: string;
  onDismiss: () => void;
}) {
  const query = useQuery({
    queryKey: ["sms", "delivery", messageId],
    queryFn: () => getDeliveryStatus(messageId),
    // 全部分片确认后即为终态，停止轮询；未确认时每 3 秒查一次
    refetchInterval: (q) => {
      const d = q.state.data;
      if (!d) return 3000;
      if (d.last_error) return false;
      if (d.parts_total > 0 && d.acks >= d.parts_total) return false;
      return 3000;
    },
    // 404 表示没有投递记录（AT 通道），重试无意义
    retry: false,
  });

  // 没有记录就不占位置——AT 通道属于这种情况
  if (query.isError) {
    const notFound =
      query.error instanceof ApiError && query.error.httpStatus === 404;
    if (notFound) return null;
  }

  const d = query.data;
  const done = d && d.parts_total > 0 && d.acks >= d.parts_total;
  const failed = Boolean(d?.last_error);

  return (
    <div
      className={cn(
        "flex items-center justify-between gap-3 rounded-lg border px-3 py-2 text-xs",
        failed && "border-destructive/40 bg-destructive/5",
      )}
    >
      <div className="min-w-0">
        {query.isPending ? (
          <span className="text-muted-foreground">正在查询投递状态…</span>
        ) : failed ? (
          <span className="text-destructive">投递失败：{d?.last_error}</span>
        ) : done ? (
          <span className="text-emerald-700 dark:text-emerald-400">
            已送达（{d?.acks}/{d?.parts_total} 分片确认）
          </span>
        ) : (
          <span className="text-muted-foreground">
            等待回执 {d?.acks ?? 0}/{d?.parts_total ?? "?"} 分片
            {d?.state ? ` · ${d.state}` : ""}
          </span>
        )}
      </div>

      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="关闭投递状态"
        onClick={onDismiss}
      >
        <X className="size-3.5" />
      </Button>
    </div>
  );
}
