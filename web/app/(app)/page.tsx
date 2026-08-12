"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { Smartphone, MessageSquare, Network, Signal } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { ErrorState } from "@/components/common/empty-state";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { listDashboardDevices } from "@/lib/api/endpoints/devices";
import { TrafficChart } from "@/components/traffic/traffic-chart";
import type { DeviceOverview } from "@/types/device";

export default function DashboardPage() {
  const devicesQuery = useQuery({
    queryKey: ["dashboard", "devices"],
    queryFn: listDashboardDevices,
    refetchInterval: 15_000,
  });

  return (
    <>
      <PageHeader title="仪表盘" description="设备与服务运行概览" />

      {devicesQuery.isError ? (
        <ErrorState
          error={devicesQuery.error}
          onRetry={() => devicesQuery.refetch()}
        />
      ) : devicesQuery.isPending ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      ) : (
        <Summary devices={devicesQuery.data} />
      )}

      <div className="mt-6">
        <TrafficChart />
      </div>

      <div className="mt-8">
        <h2 className="mb-3 text-sm font-medium text-muted-foreground">
          快捷入口
        </h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <QuickLink href="/devices" icon={Smartphone} label="设备管理" />
          <QuickLink href="/sms" icon={MessageSquare} label="短信中心" />
          <QuickLink href="/proxy" icon={Network} label="代理管理" />
        </div>
      </div>
    </>
  );
}

function Summary({ devices }: { devices: DeviceOverview[] }) {
  const total = devices.length;
  // running / healthy / 数据连接是相互独立的位，分别统计更能反映真实状态
  const online = devices.filter((d) => d.lifecycle_phase === "online").length;
  const dataConnected = devices.filter((d) => d.data_connected).length;
  const degraded = devices.filter(
    (d) => d.lifecycle_phase === "degraded" || !d.healthy,
  ).length;

  const stats = [
    { label: "设备总数", value: total, icon: Smartphone },
    { label: "在线", value: online, icon: Signal },
    { label: "已联网", value: dataConnected, icon: Network },
    { label: "异常", value: degraded, icon: Signal },
  ];

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {stats.map(({ label, value, icon: Icon }) => (
        <Card key={label}>
          <CardContent className="flex items-center justify-between">
            <div>
              <p className="text-sm text-muted-foreground">{label}</p>
              <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
            </div>
            <Icon className="size-5 text-muted-foreground" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

function QuickLink({
  href,
  icon: Icon,
  label,
}: {
  href: string;
  icon: React.ComponentType<{ className?: string }>;
  label: string;
}) {
  return (
    <Link
      href={href}
      className="flex items-center gap-3 rounded-lg border p-4 text-sm transition-colors hover:bg-accent"
    >
      <Icon className="size-4 text-muted-foreground" />
      {label}
    </Link>
  );
}
