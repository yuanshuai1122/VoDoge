"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { RefreshCw } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import { Sensitive } from "@/components/common/sensitive";
import {
  listDiscoveredDevices,
  addDeviceWithConfig,
} from "@/lib/api/endpoints/devices";
import { ApiError } from "@/lib/api/errors";
import {
  configFromDiscovered,
  type DiscoveredDevice,
} from "@/types/device-config";
import { cn } from "@/lib/utils";

/**
 * 从已发现的硬件中挑一个加入管理。
 *
 * 只走「发现 → 选择」这条路径，不提供手填 usb_path / at_port 的表单：
 * 这些值填错会绑定到错误的物理设备，而发现结果本身就是准确的。
 */
export function AddDeviceDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<DiscoveredDevice | null>(null);
  const [name, setName] = useState("");

  const query = useQuery({
    queryKey: ["devices", "discovered"],
    queryFn: listDiscoveredDevices,
    enabled: open,
  });

  const add = useMutation({
    mutationFn: (d: DiscoveredDevice) =>
      addDeviceWithConfig(configFromDiscovered(d, { name: name.trim() })),
    onSuccess: (result) => {
      // 配置写入成功但运行时启动失败也会返回 200，这时必须把 warning 说清楚
      if (result.warning) {
        toast.warning(result.warning);
      } else {
        toast.success("设备已添加");
      }
      queryClient.invalidateQueries({ queryKey: ["devices"] });
      onOpenChange(false);
      setSelected(null);
      setName("");
    },
    onError: (e) =>
      toast.error(e instanceof ApiError ? e.message : "添加失败"),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>添加设备</DialogTitle>
          <DialogDescription>
            从已发现的硬件中选择一个加入管理。未列出的设备可先执行「重新扫描」。
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          <div className="flex justify-end">
            <Button
              variant="outline"
              size="sm"
              disabled={query.isFetching}
              onClick={() => query.refetch()}
            >
              <RefreshCw
                className={cn("size-4", query.isFetching && "animate-spin")}
              />
              刷新发现结果
            </Button>
          </div>

          {query.isError ? (
            <ErrorState error={query.error} onRetry={() => query.refetch()} />
          ) : query.isPending ? (
            <Skeleton className="h-40" />
          ) : query.data.length === 0 ? (
            <EmptyState
              title="未发现硬件"
              description="确认模组已插入，且容器已透传 /dev 与 USB 设备（Windows 需 WSL + usbipd）。"
            />
          ) : (
            <div className="max-h-72 overflow-auto rounded-lg border">
              {query.data.map((d) => (
                <DiscoveredRow
                  key={d.discovery_key}
                  device={d}
                  active={selected?.discovery_key === d.discovery_key}
                  onSelect={() => setSelected(d)}
                />
              ))}
            </div>
          )}

          {selected && (
            <div className="flex flex-col gap-2">
              <Label htmlFor="device_name">设备名称（可选）</Label>
              <Input
                id="device_name"
                value={name}
                placeholder="留空则使用 IMEI"
                onChange={(e) => setName(e.target.value)}
              />
            </div>
          )}

          {selected?.degraded && (
            <Alert variant="destructive">
              <AlertDescription>
                该设备探测不到 IMEI，无法确立唯一身份，不能直接添加。
                请检查 AT 端口权限或更换 USB 口后重新扫描。
              </AlertDescription>
            </Alert>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            disabled={
              !selected ||
              selected.degraded ||
              selected.configured ||
              add.isPending
            }
            onClick={() => selected && add.mutate(selected)}
          >
            {add.isPending ? "添加中…" : "添加"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DiscoveredRow({
  device,
  active,
  onSelect,
}: {
  device: DiscoveredDevice;
  active: boolean;
  onSelect: () => void;
}) {
  const disabled = device.configured || device.degraded;

  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onSelect}
      className={cn(
        "flex w-full flex-col gap-1 border-b px-3 py-2.5 text-left transition-colors last:border-b-0",
        active && "bg-accent",
        disabled ? "cursor-not-allowed opacity-60" : "hover:bg-accent/50",
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">
          {device.net_interface || device.control_path || device.discovery_key}
        </span>
        {device.mode && <Badge variant="outline">{device.mode}</Badge>}
        {device.configured && <Badge variant="secondary">已添加</Badge>}
        {device.degraded && <Badge variant="destructive">身份不可确立</Badge>}
        {!device.network_capable && !device.degraded && (
          <Badge variant="outline">不可联网</Badge>
        )}
      </div>

      <p className="font-mono text-xs text-muted-foreground">
        IMEI <Sensitive value={device.imei} />
        {device.usb_path && ` · ${device.usb_path}`}
        {device.at_port && ` · ${device.at_port}`}
      </p>
    </button>
  );
}
