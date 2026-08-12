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

/**
 * 设备详情。
 *
 * 走 query 参数而非 /devices/[id]：静态导出不支持没有 generateStaticParams 的
 * 动态路由，而设备 ID 只有运行时才知道，无法在构建期枚举。
 */
const TABS = [
  { value: "overview", label: "概览" },
  { value: "esim", label: "eSIM" },
  { value: "at", label: "AT" },
  { value: "ussd", label: "USSD" },
  { value: "operator", label: "选网" },
  { value: "card-policy", label: "卡策略" },
  { value: "config", label: "配置" },
] as const;

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

  const deviceId = params.get("id") ?? "";
  const tab = params.get("tab") ?? "overview";

  if (!deviceId) {
    return (
      <>
        <PageHeader title="设备详情" />
        <EmptyState
          title="缺少设备参数"
          description="请从设备列表进入。"
          action={
            <Link href="/devices" className={buttonVariants({ size: "sm" })}>
              返回设备列表
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
        title="设备详情"
        description={deviceId}
        actions={
          <Link
            href="/devices"
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            <ArrowLeft className="size-4" />
            返回列表
          </Link>
        }
      />

      <Tabs value={tab} onValueChange={(v) => v && setTab(v)}>
        <TabsList>
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value}>
              {t.label}
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
