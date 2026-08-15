"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { Smartphone, MessageSquare, Network, Signal } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { ErrorState } from "@/components/common/empty-state";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { listDevices } from "@/lib/api/endpoints/devices";
import { TrafficChart } from "@/components/traffic/traffic-chart";
import type { DeviceOverview } from "@/types/device";
import { useT } from "@/lib/i18n";

export default function DashboardPage() {
  const t = useT();
  // 刻意用 /devices 而非 /dashboard/devices：后者是精简快照，
  // 缺 lifecycle_phase 与 data_connected，下面的统计会恒为 0。
  // 复用设备页的 queryKey，两处共享同一份缓存。
  const devicesQuery = useQuery({
    queryKey: ["devices", "list"],
    queryFn: listDevices,
    refetchInterval: 15_000,
  });

  return (
    <>
      <PageHeader title={t("dash.title")} description={t("dash.desc")} />

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
        <Summary devices={devicesQuery.data.devices} />
      )}

      <div className="mt-6">
        <TrafficChart />
      </div>

      <div className="mt-8">
        <h2 className="mb-3 text-sm font-medium text-muted-foreground">
          {t("dash.shortcuts")}
        </h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <QuickLink href="/devices" icon={Smartphone} label={t("nav.devices")} />
          <QuickLink href="/sms" icon={MessageSquare} label={t("nav.sms")} />
          <QuickLink href="/proxy" icon={Network} label={t("nav.proxy")} />
        </div>
      </div>
    </>
  );
}

function Summary({ devices }: { devices: DeviceOverview[] }) {
  const t = useT();
  const total = devices.length;
  // running / healthy / 数据连接是相互独立的位，分别统计更能反映真实状态
  const online = devices.filter((d) => d.lifecycle_phase === "online").length;
  const dataConnected = devices.filter((d) => d.data_connected).length;
  const degraded = devices.filter(
    (d) => d.lifecycle_phase === "degraded" || !d.healthy,
  ).length;

  const stats = [
    { label: t("dash.total"), value: total, icon: Smartphone },
    { label: t("dash.online"), value: online, icon: Signal },
    { label: t("dash.dataOn"), value: dataConnected, icon: Network },
    { label: t("dash.abnormal"), value: degraded, icon: Signal },
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
