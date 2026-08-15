"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "@/components/layout/page-header";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import {
  listPlugins,
  pluginAssetURL,
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

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }
  if (query.isPending) {
    return <Skeleton className="h-96" />;
  }

  const plugin = query.data?.find((item) => item.id === pluginId && item.enabled);
  const contribution = plugin?.contributions.find(
    (item) => item.id === contributionId && item.location === "sidebar",
  );
  if (!plugin || !contribution) {
    return (
      <EmptyState
        title={t("plugin.unavailable")}
        description={t("plugin.unavailableHint")}
      />
    );
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
        src={pluginAssetURL(plugin.id, contribution.entry)}
        className="h-[calc(100vh-10rem)] min-h-[560px] w-full rounded-xl border bg-background"
        sandbox="allow-scripts allow-forms allow-same-origin"
        allow="microphone; autoplay"
      />
    </>
  );
}
