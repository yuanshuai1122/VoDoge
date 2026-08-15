"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import {
  listContacts,
  nextContactsCursor,
  SMS_PAGE_SIZE,
  type ContactsCursor,
} from "@/lib/api/endpoints/sms";
import type { SMSContact } from "@/types/sms";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/format";
import { LaneBadge } from "@/components/common/lane-badge";
import type { DeviceLane } from "@/lib/lane";

export interface ContactKey {
  peer: string;
  imsi: string;
  device_id: string;
  device_name: string;
  local_phone: string;
}

export function ContactList({
  selected,
  onSelect,
  lane = "",
}: {
  selected: ContactKey | null;
  onSelect: (c: ContactKey) => void;
  lane?: DeviceLane;
}) {
  // 后端无短信 SSE，只能轮询；会话列表变化频率低，15s 足够
  const query = useInfiniteQuery({
    queryKey: ["sms", "contacts", lane],
    queryFn: ({ pageParam }) =>
      listContacts({ cursor: pageParam, lane: lane || undefined }),
    initialPageParam: undefined as ContactsCursor | undefined,
    getNextPageParam: (lastPage) => nextContactsCursor(lastPage, SMS_PAGE_SIZE),
    refetchInterval: 15_000,
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }

  if (query.isPending) {
    return (
      <div className="flex flex-col gap-2 p-2">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-16" />
        ))}
      </div>
    );
  }

  const contacts = query.data.pages.flat();

  if (contacts.length === 0) {
    return (
      <EmptyState
        title="暂无短信会话"
        description="设备收到或发出短信后会在这里显示。"
        className="m-2"
      />
    );
  }

  return (
    <div className="flex flex-col">
      {contacts.map((c) => (
        <ContactRow
          key={`${c.imsi}:${c.peer}`}
          contact={c}
          active={selected?.peer === c.peer && selected?.imsi === c.imsi}
          onSelect={onSelect}
        />
      ))}

      {query.hasNextPage && (
        <div className="p-2">
          <Button
            variant="outline"
            size="sm"
            className="w-full"
            disabled={query.isFetchingNextPage}
            onClick={() => query.fetchNextPage()}
          >
            {query.isFetchingNextPage ? "加载中…" : "加载更多"}
          </Button>
        </div>
      )}
    </div>
  );
}

function ContactRow({
  contact,
  active,
  onSelect,
}: {
  contact: SMSContact;
  active: boolean;
  onSelect: (c: ContactKey) => void;
}) {
  return (
    <button
      type="button"
      onClick={() =>
        onSelect({
          peer: contact.peer,
          imsi: contact.imsi,
          device_id: contact.device_id,
          device_name: contact.device_name,
          local_phone: contact.local_phone,
        })
      }
      className={cn(
        "flex w-full flex-col gap-1 border-b px-3 py-2.5 text-left transition-colors",
        active ? "bg-accent" : "hover:bg-accent/50",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="truncate text-sm font-medium">{contact.peer}</span>
        <span className="shrink-0 text-xs text-muted-foreground">
          {formatRelativeTime(contact.last_timestamp)}
        </span>
      </div>

      <p className="line-clamp-1 text-xs text-muted-foreground">
        {contact.last_content || "（无内容）"}
      </p>

      <div className="flex items-center gap-1.5">
        {/* 设备名可能为空：短信按 ICCID 归属，换卡后原设备未必在线 */}
        {contact.device_name && (
          <span className="truncate text-[11px] text-muted-foreground">
            {contact.device_name}
          </span>
        )}
        <LaneBadge lane={contact.lane} className="h-4 px-1.5 text-[10px]" />
        {contact.unread_count > 0 && (
          <Badge variant="default" className="h-4 px-1.5 text-[10px]">
            {contact.unread_count}
          </Badge>
        )}
      </div>
    </button>
  );
}
