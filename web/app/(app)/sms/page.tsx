"use client";

import { useState } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { ContactList, type ContactKey } from "@/components/sms/contact-list";
import { ThreadView } from "@/components/sms/thread-view";

export default function SmsPage() {
  const [selected, setSelected] = useState<ContactKey | null>(null);

  return (
    <>
      <PageHeader
        title="短信中心"
        description="短信按 ICCID 归属：更换 eSIM Profile 后，历史记录跟随卡而非设备。"
      />

      <div className="flex h-[calc(100svh-13rem)] overflow-hidden rounded-lg border">
        <div className="w-72 shrink-0 overflow-auto border-r">
          <ContactList selected={selected} onSelect={setSelected} />
        </div>

        <div className="min-w-0 flex-1">
          {selected ? (
            <ThreadView key={`${selected.imsi}:${selected.peer}`} contact={selected} />
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
