"use client";

import { useState } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { ContactList, type ContactKey } from "@/components/sms/contact-list";
import { ThreadView } from "@/components/sms/thread-view";
import { cn } from "@/lib/utils";
import { DEVICE_LANES, type DeviceLane } from "@/lib/lane";

export default function SmsPage() {
  const [selected, setSelected] = useState<ContactKey | null>(null);
  const [lane, setLane] = useState<DeviceLane>("");

  return (
    <>
      <PageHeader
        title="短信中心"
        description="短信按 ICCID 归属：更换 eSIM Profile 后，历史记录跟随卡而非设备。"
      />

      <div
        className="mb-3 flex gap-1"
        role="tablist"
        aria-label="按线路过滤短信"
      >
        {DEVICE_LANES.map((opt) => {
          const active = lane === opt.value;
          return (
            <button
              key={opt.value || "all"}
              type="button"
              role="tab"
              aria-selected={active}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm transition-colors",
                active
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted text-muted-foreground hover:bg-accent",
              )}
              onClick={() => {
                setLane(opt.value);
                setSelected(null);
              }}
            >
              {opt.value === "" ? "全部" : opt.label}
            </button>
          );
        })}
      </div>

      <div className="flex h-[calc(100svh-16rem)] overflow-hidden rounded-lg border md:h-[calc(100svh-15rem)]">
        <div
          className={cn(
            "w-full shrink-0 overflow-auto md:w-72 md:border-r",
            selected && "hidden md:block",
          )}
        >
          <ContactList
            selected={selected}
            onSelect={setSelected}
            lane={lane}
          />
        </div>

        <div
          className={cn(
            "min-w-0 flex-1",
            !selected && "hidden md:block",
          )}
        >
          {selected ? (
            <ThreadView
              key={`${selected.imsi}:${selected.peer}`}
              contact={selected}
              onBack={() => setSelected(null)}
            />
          ) : (
            <div className="flex h-full items-center justify-center">
              <p className="text-sm text-muted-foreground">
                选择左侧会话查看短信
              </p>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
