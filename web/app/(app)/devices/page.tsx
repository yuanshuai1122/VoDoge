"use client";

import { PageHeader } from "@/components/layout/page-header";
import { EmptyState } from "@/components/common/empty-state";

export default function DevicesPage() {
  return (
    <>
      <PageHeader title="设备管理" description="模组发现、状态与配置" />
      <EmptyState
        title="设备管理开发中"
        description="计划阶段 5：设备列表、9 种生命周期状态灯、详情 Tabs 与概览 SSE 实时流。"
      />
    </>
  );
}
