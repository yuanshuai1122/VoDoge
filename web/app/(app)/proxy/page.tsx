"use client";

import { PageHeader } from "@/components/layout/page-header";
import { EmptyState } from "@/components/common/empty-state";

export default function ProxyPage() {
  return (
    <>
      <PageHeader title="代理管理" description="本机实例与上游代理" />
      <EmptyState
        title="代理管理开发中"
        description="计划阶段 8：实例增删启停、上游代理探测与国家路由规则。"
      />
    </>
  );
}
