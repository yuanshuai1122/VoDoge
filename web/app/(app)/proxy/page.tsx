"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Play, Square, RotateCw, Trash2, Activity, Plus, Pencil } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  getProxyOverview,
  startInstance,
  stopInstance,
  restartInstance,
  listUpstreamProxies,
  deleteUpstreamProxy,
  probeUpstreamProxy,
  type ProxyInstanceStatus,
  type ProxyInstance,
  type UpstreamProxy,
} from "@/lib/api/endpoints/proxy";
import { ApiError } from "@/lib/api/errors";
import { UpstreamDialog } from "@/components/proxy/upstream-dialog";
import { CountryRules } from "@/components/proxy/country-rules";
import { InstanceDialog } from "@/components/proxy/instance-dialog";

export default function ProxyPage() {
  return (
    <>
      <PageHeader title="代理管理" description="本机代理实例与上游代理" />

      <Tabs defaultValue="instances">
        <TabsList>
          <TabsTrigger value="instances">本机实例</TabsTrigger>
          <TabsTrigger value="upstream">上游代理</TabsTrigger>
          <TabsTrigger value="countries">国家路由</TabsTrigger>
        </TabsList>

        <TabsContent value="instances" className="mt-4">
          <InstancesPanel />
        </TabsContent>

        <TabsContent value="upstream" className="mt-4">
          <UpstreamPanel />
        </TabsContent>

        <TabsContent value="countries" className="mt-4">
          <CountryRules />
        </TabsContent>
      </Tabs>
    </>
  );
}

function InstancesPanel() {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<ProxyInstance | null>(null);

  const openCreate = () => {
    setEditing(null);
    setDialogOpen(true);
  };
  const openEdit = (i: ProxyInstance) => {
    setEditing(i);
    setDialogOpen(true);
  };

  const query = useQuery({
    queryKey: ["proxy", "overview"],
    queryFn: getProxyOverview,
    refetchInterval: 10_000,
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["proxy"] });
  const onError = (e: unknown) =>
    toast.error(e instanceof ApiError ? e.message : "操作失败");

  const start = useMutation({
    mutationFn: startInstance,
    onSuccess: () => {
      toast.success("已启动");
      invalidate();
    },
    onError,
  });
  const stop = useMutation({
    mutationFn: stopInstance,
    onSuccess: () => {
      toast.success("已停止");
      invalidate();
    },
    onError,
  });
  const restart = useMutation({
    mutationFn: restartInstance,
    onSuccess: () => {
      toast.success("已重启");
      invalidate();
    },
    onError,
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }
  if (query.isPending) return <Skeleton className="h-48" />;

  const { instances, devices, status } = query.data;
  const statusById = new Map<string, ProxyInstanceStatus>(
    status.map((s) => [s.id, s]),
  );
  const deviceById = new Map(devices.map((d) => [d.id, d]));

  if (instances.length === 0) {
    return (
      <>
        <EmptyState
          title="暂无代理实例"
          description="每个实例绑定一台设备，出站流量走该设备的网卡。"
          action={
            <Button size="sm" disabled={devices.length === 0} onClick={openCreate}>
              <Plus className="size-4" />
              新增实例
            </Button>
          }
        />
        <InstanceDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          editing={editing}
          allInstances={instances}
          devices={devices}
        />
      </>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex justify-end">
        <Button size="sm" disabled={devices.length === 0} onClick={openCreate}>
          <Plus className="size-4" />
          新增实例
        </Button>
      </div>

      <div className="overflow-x-auto rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>实例</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>协议</TableHead>
            <TableHead>监听</TableHead>
            <TableHead>绑定设备</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>

        <TableBody>
          {instances.map((inst) => {
            const st = statusById.get(inst.id);
            const dev = deviceById.get(inst.device_id);
            const running = st?.running ?? false;
            const busy =
              start.isPending || stop.isPending || restart.isPending;

            return (
              <TableRow key={inst.id}>
                <TableCell>
                  <p className="font-medium">{inst.name || inst.id}</p>
                  <p className="text-xs text-muted-foreground">
                    {inst.auth_enabled ? `需认证 · ${inst.username}` : "无认证"}
                  </p>
                </TableCell>

                <TableCell>
                  <Badge variant={running ? "default" : "secondary"}>
                    {running ? "运行中" : "已停止"}
                  </Badge>
                  {/* 上次退出的错误是排障的关键信息，不要吞掉 */}
                  {st?.last_error && (
                    <p className="mt-1 max-w-52 truncate text-xs text-destructive" title={st.last_error}>
                      {st.last_error}
                    </p>
                  )}
                </TableCell>

                <TableCell className="text-sm uppercase">
                  {inst.mode || st?.mode || "-"}
                </TableCell>

                <TableCell className="text-sm tabular-nums">
                  {inst.listen_addr || st?.listen_addr || "-"}:
                  {inst.listen_port || st?.listen_port || "-"}
                </TableCell>

                <TableCell className="text-sm">
                  {dev ? (
                    <>
                      {dev.name || dev.id}
                      <span className="block text-xs text-muted-foreground">
                        {dev.interface || st?.interface || ""}
                      </span>
                    </>
                  ) : (
                    inst.device_id || "-"
                  )}
                </TableCell>

                <TableCell>
                  <div className="flex justify-end gap-1">
                    {running ? (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="停止"
                        disabled={busy}
                        onClick={() => stop.mutate(inst.id)}
                      >
                        <Square className="size-4" />
                      </Button>
                    ) : (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="启动"
                        disabled={busy}
                        onClick={() => start.mutate(inst.id)}
                      >
                        <Play className="size-4" />
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="重启"
                      disabled={busy}
                      onClick={() => restart.mutate(inst.id)}
                    >
                      <RotateCw className="size-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="编辑"
                      onClick={() => openEdit(inst)}
                    >
                      <Pencil className="size-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      </div>

      <InstanceDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        editing={editing}
        allInstances={instances}
        devices={devices}
      />
    </div>
  );
}

function UpstreamPanel() {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<UpstreamProxy | null>(null);

  const openCreate = () => {
    setEditing(null);
    setDialogOpen(true);
  };
  const openEdit = (p: UpstreamProxy) => {
    setEditing(p);
    setDialogOpen(true);
  };

  const query = useQuery({
    queryKey: ["proxy", "upstream"],
    queryFn: listUpstreamProxies,
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["proxy", "upstream"] });
  const onError = (e: unknown) =>
    toast.error(e instanceof ApiError ? e.message : "操作失败");

  const probe = useMutation({
    mutationFn: probeUpstreamProxy,
    onSuccess: (result) => {
      toast.success(`探测完成：${JSON.stringify(result)}`);
      invalidate();
    },
    onError,
  });

  const remove = useMutation({
    mutationFn: deleteUpstreamProxy,
    onSuccess: () => {
      toast.success("已删除");
      invalidate();
    },
    onError,
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }
  if (query.isPending) return <Skeleton className="h-48" />;

  if (query.data.length === 0) {
    return (
      <>
        <EmptyState
          title="暂无上游代理"
          description="上游代理用于二次转发出站流量。"
          action={
            <Button size="sm" onClick={openCreate}>
              <Plus className="size-4" />
              新增上游代理
            </Button>
          }
        />
        <UpstreamDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          editing={editing}
        />
      </>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex justify-end">
        <Button size="sm" onClick={openCreate}>
          <Plus className="size-4" />
          新增上游代理
        </Button>
      </div>

      <div className="overflow-x-auto rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>名称</TableHead>
            <TableHead>地址</TableHead>
            <TableHead>认证</TableHead>
            <TableHead>状态</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>

        <TableBody>
          {query.data.map((p) => (
            <TableRow key={p.id}>
              <TableCell className="font-medium">{p.name || p.id}</TableCell>
              <TableCell className="font-mono text-xs">{p.addr}</TableCell>
              <TableCell className="text-sm">{p.username || "-"}</TableCell>
              <TableCell>
                <Badge variant={p.enabled ? "default" : "secondary"}>
                  {p.enabled ? "启用" : "停用"}
                </Badge>
              </TableCell>
              <TableCell>
                <div className="flex justify-end gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="探测"
                    disabled={probe.isPending}
                    onClick={() => probe.mutate(p.id)}
                  >
                    <Activity className="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="编辑"
                    onClick={() => openEdit(p)}
                  >
                    <Pencil className="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="删除"
                    disabled={remove.isPending}
                    onClick={() => {
                      if (confirm(`确定删除上游代理「${p.name || p.id}」？`)) {
                        remove.mutate(p.id);
                      }
                    }}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      </div>

      <UpstreamDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        editing={editing}
      />
    </div>
  );
}
