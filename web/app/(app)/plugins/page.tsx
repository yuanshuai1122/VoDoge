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
        title="插件不可用"
        description="插件可能已被禁用、卸载或没有注册此页面。"
      />
    );
  }

  const title = contribution.label_zh || contribution.label;
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
