"use client";

import { Suspense } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { EmptyState } from "@/components/common/empty-state";
import { buttonVariants } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import { DeviceOverviewTab } from "@/components/devices/device-overview-tab";
import { EsimTab } from "@/components/devices/esim-tab";
import { AtTab } from "@/components/devices/at-tab";
import { UssdTab } from "@/components/devices/ussd-tab";
import { CardPolicyTab } from "@/components/devices/card-policy-tab";
import { ConfigTab } from "@/components/devices/config-tab";
import { OperatorTab } from "@/components/devices/operator-tab";
import { useT, type MessageKey } from "@/lib/i18n";

/**
 * 设备详情。
 *
 * 走 query 参数而非 /devices/[id]：静态导出不支持没有 generateStaticParams 的
 * 动态路由，而设备 ID 只有运行时才知道，无法在构建期枚举。
 */
const TABS: { value: string; labelKey: MessageKey }[] = [
  { value: "overview", labelKey: "detail.tab.overview" },
  { value: "esim", labelKey: "detail.tab.esim" },
  { value: "at", labelKey: "detail.tab.at" },
  { value: "ussd", labelKey: "detail.tab.ussd" },
  { value: "operator", labelKey: "detail.tab.operator" },
  { value: "card-policy", labelKey: "detail.tab.policy" },
  { value: "config", labelKey: "detail.tab.config" },
];

export default function DeviceDetailPage() {
  return (
    // useSearchParams 在静态导出下必须包在 Suspense 内
    <Suspense fallback={<Skeleton className="h-96" />}>
      <DeviceDetail />
    </Suspense>
  );
}

function DeviceDetail() {
  const router = useRouter();
  const params = useSearchParams();
  const t = useT();

  const deviceId = params.get("id") ?? "";
  const tab = params.get("tab") ?? "overview";

  if (!deviceId) {
    return (
      <>
        <PageHeader title={t("detail.title")} />
        <EmptyState
          title={t("detail.missing")}
          description={t("detail.missingHint")}
          action={
            <Link href="/devices" className={buttonVariants({ size: "sm" })}>
              {t("detail.back")}
            </Link>
          }
        />
      </>
    );
  }

  // Tab 写进 URL，刷新与分享都能保持位置
  const setTab = (next: string) => {
    const sp = new URLSearchParams(params.toString());
    sp.set("tab", next);
    router.replace(`/devices/detail?${sp.toString()}`);
  };

  return (
    <>
      <PageHeader
        title={t("detail.title")}
        description={deviceId}
        actions={
          <Link
            href="/devices"
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            <ArrowLeft className="size-4" />
            {t("detail.back")}
          </Link>
        }
      />

      <Tabs value={tab} onValueChange={(v) => v && setTab(v)}>
        <TabsList>
          {TABS.map((item) => (
            <TabsTrigger key={item.value} value={item.value}>
              {t(item.labelKey)}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="overview" className="mt-4">
          <DeviceOverviewTab deviceId={deviceId} />
        </TabsContent>

        <TabsContent value="esim" className="mt-4">
          <EsimTab deviceId={deviceId} />
        </TabsContent>

        <TabsContent value="at" className="mt-4">
          <AtTab deviceId={deviceId} />
        </TabsContent>

        <TabsContent value="ussd" className="mt-4">
          <UssdTab deviceId={deviceId} />
        </TabsContent>

        <TabsContent value="operator" className="mt-4">
          <OperatorTab deviceId={deviceId} />
        </TabsContent>

        <TabsContent value="card-policy" className="mt-4">
          <CardPolicyTab deviceId={deviceId} />
        </TabsContent>

        <TabsContent value="config" className="mt-4">
          <ConfigTab deviceId={deviceId} />
        </TabsContent>
      </Tabs>
    </>
  );
}
