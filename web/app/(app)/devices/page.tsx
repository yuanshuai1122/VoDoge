"use client";

import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { RefreshCw, RotateCw, Trash2 } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  DeviceStatusBadge,
  SignalIndicator,
} from "@/components/devices/device-status-badge";
import {
  listDevices,
  rescanDevices,
  refreshDevice,
  deleteDevice,
} from "@/lib/api/endpoints/devices";
import { ApiError } from "@/lib/api/errors";
import { maskIdentifier } from "@/lib/format";

export default function DevicesPage() {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ["devices", "list"],
    queryFn: listDevices,
    refetchInterval: 15_000,
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["devices"] });

  const rescan = useMutation({
    mutationFn: rescanDevices,
    onSuccess: () => {
      toast.success("已触发重新扫描");
      invalidate();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "扫描失败"),
  });

  const refresh = useMutation({
    mutationFn: refreshDevice,
    onSuccess: () => {
      toast.success("已刷新设备信息");
      invalidate();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "刷新失败"),
  });

  const remove = useMutation({
    mutationFn: deleteDevice,
    onSuccess: () => {
      toast.success("设备已删除");
      invalidate();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "删除失败"),
  });

  const devices = query.data?.devices ?? [];
  const limit = query.data?.device_limit;

  return (
    <>
      <PageHeader
        title="设备管理"
        description={
          limit != null
            ? `已接入 ${devices.length} / ${limit} 台设备`
            : "模组发现、状态与配置"
        }
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={rescan.isPending}
            onClick={() => rescan.mutate()}
          >
            <RotateCw
              className={rescan.isPending ? "size-4 animate-spin" : "size-4"}
            />
            重新扫描
          </Button>
        }
      />

      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => query.refetch()} />
      ) : query.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-14" />
          ))}
        </div>
      ) : devices.length === 0 ? (
        <EmptyState
          title="暂无设备"
          description="插入模组后点击「重新扫描」，或确认容器已透传 /dev 与 USB 设备。"
          action={
            <Button size="sm" onClick={() => rescan.mutate()}>
              重新扫描
            </Button>
          }
        />
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>设备</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>信号</TableHead>
                <TableHead>运营商</TableHead>
                <TableHead>出口 IP</TableHead>
                <TableHead>ICCID</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>

            <TableBody>
              {devices.map((d) => (
                <TableRow key={d.id}>
                  <TableCell>
                    <Link
                      href={`/devices/detail?id=${encodeURIComponent(d.id)}`}
                      className="font-medium hover:underline"
                    >
                      {d.name || d.id}
                    </Link>
                    <p className="text-xs text-muted-foreground">
                      {d.interface || d.control_device || d.id}
                    </p>
                  </TableCell>

                  <TableCell>
                    <DeviceStatusBadge device={d} showDetail />
                  </TableCell>

                  <TableCell>
                    <SignalIndicator rsrp={d.modem?.signal_rsrp} />
                  </TableCell>

                  <TableCell className="text-sm">
                    {d.modem?.operator || "-"}
                  </TableCell>

                  <TableCell className="text-sm tabular-nums">
                    {d.public_ip || "-"}
                  </TableCell>

                  {/* ICCID 默认打码，避免截图/共享时泄漏 */}
                  <TableCell className="font-mono text-xs">
                    {maskIdentifier(d.modem?.iccid)}
                  </TableCell>

                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="刷新"
                        disabled={refresh.isPending}
                        onClick={() => refresh.mutate(d.id)}
                      >
                        <RefreshCw className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="删除"
                        disabled={remove.isPending}
                        onClick={() => {
                          if (
                            confirm(`确定删除设备「${d.name || d.id}」？该操作不可撤销。`)
                          ) {
                            remove.mutate(d.id);
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
      )}
    </>
  );
}
