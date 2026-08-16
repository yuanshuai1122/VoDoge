"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "@/components/layout/page-header";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import {
  createPluginSession,
  listPlugins,
} from "@/lib/api/endpoints/extensions";
import { pluginLabel, useLocale, useT } from "@/lib/i18n";

export default function PluginPage() {
  return (
    <Suspense fallback={<Skeleton className="h-96" />}>
      <PluginFrame />
    </Suspense>
  );
}

function PluginFrame() {
  const params = useSearchParams();
  const pluginId = params.get("plugin") ?? "";
  const contributionId = params.get("contribution") ?? "";
  const t = useT();
  const locale = useLocale();

  const query = useQuery({
    queryKey: ["extensions"],
    queryFn: listPlugins,
  });
  const plugin = query.data?.find((item) => item.id === pluginId && item.enabled);
  const contribution = plugin?.contributions.find(
    (item) => item.id === contributionId && item.location === "sidebar",
  );
  const runtime = useQuery({
    queryKey: ["extension-session", pluginId, contributionId],
    queryFn: () => createPluginSession(pluginId, contributionId),
    enabled: Boolean(plugin && contribution),
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  });
  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }
  if (query.isPending) {
    return <Skeleton className="h-96" />;
  }
  if (!plugin || !contribution) {
    return (
      <EmptyState
        title={t("plugin.unavailable")}
        description={t("plugin.unavailableHint")}
      />
    );
  }
  if (runtime.isError) {
    return <ErrorState error={runtime.error} onRetry={() => runtime.refetch()} />;
  }
  if (runtime.isPending || !runtime.data) {
    return <Skeleton className="h-96" />;
  }

  const title = pluginLabel(locale, contribution);
  return (
    <>
      <PageHeader
        title={title}
        description={`${plugin.name} · ${plugin.version}`}
      />
      <iframe
        title={title}
        src={runtime.data.launch_url}
        className="h-[calc(100vh-10rem)] min-h-[560px] w-full rounded-xl border bg-background"
        sandbox="allow-scripts allow-forms allow-same-origin"
        allow="microphone; autoplay"
      />
    </>
  );
}
