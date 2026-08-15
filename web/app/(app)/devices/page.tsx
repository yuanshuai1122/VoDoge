"use client";

import { useState } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { RefreshCw, RotateCw, Trash2, Plus } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
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
import { Sensitive } from "@/components/common/sensitive";
import { AddDeviceDialog } from "@/components/devices/add-device-dialog";
import { LaneBadge } from "@/components/common/lane-badge";
import { useT } from "@/lib/i18n";

export default function DevicesPage() {
  const queryClient = useQueryClient();
  const [addOpen, setAddOpen] = useState(false);
  const t = useT();

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
      toast.success(t("devices.rescanned"));
      invalidate();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : t("devices.scanFailed")),
  });

  const refresh = useMutation({
    mutationFn: refreshDevice,
    onSuccess: () => {
      toast.success(t("devices.refreshed"));
      invalidate();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : t("devices.refreshFailed")),
  });

  const remove = useMutation({
    mutationFn: deleteDevice,
    onSuccess: () => {
      toast.success(t("devices.deleted"));
      invalidate();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : t("devices.deleteFailed")),
  });

  const devices = query.data?.devices ?? [];
  const limit = query.data?.device_limit;

  return (
    <>
      <PageHeader
        title={t("devices.title")}
        description={
          limit != null
            ? t("devices.count", { n: devices.length, limit })
            : t("devices.desc")
        }
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={rescan.isPending}
              onClick={() => rescan.mutate()}
            >
              <RotateCw
                className={rescan.isPending ? "size-4 animate-spin" : "size-4"}
              />
              {t("devices.rescan")}
            </Button>
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus className="size-4" />
              {t("devices.add")}
            </Button>
          </>
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
          title={t("devices.empty")}
          description={t("devices.emptyHint")}
          action={
            <Button size="sm" onClick={() => setAddOpen(true)}>
              {t("devices.add")}
            </Button>
          }
        />
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("devices.colDevice")}</TableHead>
                <TableHead>{t("devices.colStatus")}</TableHead>
                <TableHead>{t("devices.colSignal")}</TableHead>
                <TableHead>{t("devices.colOperator")}</TableHead>
                <TableHead>{t("devices.colIp")}</TableHead>
                <TableHead>{t("devices.colIccid")}</TableHead>
                <TableHead className="text-right">{t("devices.colActions")}</TableHead>
              </TableRow>
            </TableHeader>

            <TableBody>
              {devices.map((d) => (
                <TableRow key={d.id}>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Link
                        href={`/devices/detail?id=${encodeURIComponent(d.id)}`}
                        className="font-medium hover:underline"
                      >
                        {d.name || d.id}
                      </Link>
                      <LaneBadge lane={d.lane} />
                      {d.device_backend === "pcsc" && (
                        <Badge variant="outline">读卡器</Badge>
                      )}
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {d.active_esim_profile_name
                        ? `当前卡 ${d.active_esim_profile_name}`
                        : d.interface || d.control_device || d.id}
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
                    <Sensitive value={d.modem?.iccid} />
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
      <AddDeviceDialog open={addOpen} onOpenChange={setAddOpen} />
    </>
  );
}
