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
import { listReaders } from "@/lib/api/endpoints/readers";
import { ApiError } from "@/lib/api/errors";
import {
  configFromDiscovered,
  type DiscoveredDevice,
} from "@/types/device-config";
import { cn } from "@/lib/utils";
import { DEVICE_LANES, type DeviceLane } from "@/lib/lane";
import { useT } from "@/lib/i18n";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

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
  const [selectedReader, setSelectedReader] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [lane, setLane] = useState<DeviceLane>("");
  const t = useT();

  const query = useQuery({
    queryKey: ["devices", "discovered"],
    queryFn: listDiscoveredDevices,
    enabled: open,
  });

  const readers = useQuery({
    queryKey: ["readers"],
    queryFn: listReaders,
    enabled: open,
  });

  const add = useMutation({
    mutationFn: (d: DiscoveredDevice) =>
      addDeviceWithConfig(
        configFromDiscovered(d, { name: name.trim(), lane: lane || undefined }),
      ),
    onSuccess: (result) => {
      // 配置写入成功但运行时启动失败也会返回 200，这时必须把 warning 说清楚
      if (result.warning) {
        toast.warning(result.warning);
      } else {
        toast.success(t("devices.addOk"));
      }
      queryClient.invalidateQueries({ queryKey: ["devices"] });
      onOpenChange(false);
      setSelected(null);
      setSelectedReader(null);
      setName("");
      setLane("");
    },
    onError: (e) =>
      toast.error(e instanceof ApiError ? e.message : t("devices.addFailed")),
  });

  const addReader = useMutation({
    mutationFn: (readerName: string) => {
      const slug = readerName
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "")
        .slice(0, 40);
      return addDeviceWithConfig({
        id: `reader-${slug || "ccid"}`,
        name: name.trim() || readerName,
        modem_imei: "",
        usb_path: "",
        at_port: "",
        proxy_port: 0,
        interface: "",
        sms_enabled: true,
        network_enabled: false,
        vowifi_enabled: true,
        device_backend: "pcsc",
        reader_name: readerName,
        lane: lane || undefined,
      });
    },
    onSuccess: (result) => {
      if (result.warning) toast.warning(result.warning);
      else toast.success(t("devices.readerAdded"));
      queryClient.invalidateQueries({ queryKey: ["devices"] });
      queryClient.invalidateQueries({ queryKey: ["readers"] });
      onOpenChange(false);
      setSelected(null);
      setSelectedReader(null);
      setName("");
      setLane("");
    },
    onError: (e) =>
      toast.error(e instanceof ApiError ? e.message : t("devices.addFailed")),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("add.title")}</DialogTitle>
          <DialogDescription>{t("add.hint")}</DialogDescription>
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
              {t("add.refresh")}
            </Button>
          </div>

          {query.isError ? (
            <ErrorState error={query.error} onRetry={() => query.refetch()} />
          ) : query.isPending ? (
            <Skeleton className="h-40" />
          ) : query.data.length === 0 ? (
            <EmptyState
              title={t("add.none")}
              description={t("add.noneHint")}
            />
          ) : (
            <div className="max-h-72 overflow-auto rounded-lg border">
              {query.data.map((d) => (
                <DiscoveredRow
                  key={d.discovery_key}
                  device={d}
                  active={selected?.discovery_key === d.discovery_key}
                  onSelect={() => {
                    setSelected(d);
                    setSelectedReader(null);
                  }}
                />
              ))}
            </div>
          )}

          {(selected || selectedReader) && (
            <div className="flex flex-col gap-3">
              <div className="flex flex-col gap-2">
                <Label htmlFor="device_name">{t("add.nameOptional")}</Label>
                <Input
                  id="device_name"
                  value={name}
                  placeholder={t("add.namePh")}
                  onChange={(e) => setName(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="add_lane">{t("add.lane")}</Label>
                <Select
                  value={lane}
                  onValueChange={(v) => setLane((v ?? "") as DeviceLane)}
                >
                  <SelectTrigger id="add_lane">
                    <SelectValue placeholder={t("lane.none")} />
                  </SelectTrigger>
                  <SelectContent>
                    {DEVICE_LANES.map((opt) => (
                      <SelectItem key={opt.value || "none"} value={opt.value}>
                        {t(opt.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">
                  {t("add.laneHint")}
                </p>
              </div>
            </div>
          )}

          <ReadersPicker
            status={readers.data}
            loading={readers.isPending}
            selectedName={selectedReader}
            onSelect={(n) => {
              setSelectedReader(n);
              setSelected(null);
            }}
          />

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
              add.isPending ||
              addReader.isPending ||
              (!selectedReader &&
                (!selected || selected.degraded || selected.configured))
            }
            onClick={() => {
              if (selectedReader) addReader.mutate(selectedReader);
              else if (selected) add.mutate(selected);
            }}
          >
            {add.isPending || addReader.isPending ? "添加中…" : "添加"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ReadersPicker({
  status,
  loading,
  selectedName,
  onSelect,
}: {
  status?: {
    daemon: string;
    message: string;
    readers: { name: string; claimed_id?: string }[];
  };
  loading: boolean;
  selectedName: string | null;
  onSelect: (name: string) => void;
}) {
  if (loading && !status) {
    return (
      <p className="text-xs text-muted-foreground">正在探测 USB 读卡器…</p>
    );
  }
  if (!status) return null;

  if (status.daemon === "missing" || status.daemon === "error") {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          读卡器：{status.message || "未检测到 pcscd"}。写卡需要先启动 pcscd。
        </AlertDescription>
      </Alert>
    );
  }

  const list = status.readers.filter((r) => r.name);
  if (list.length === 0) {
    return (
      <Alert>
        <AlertDescription>
          读卡器：pcscd 在运行，当前没有读卡器。
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm font-medium">USB 读卡器</p>
      <div className="rounded-lg border">
        {list.map((r) => {
          const claimed = Boolean(r.claimed_id);
          return (
            <button
              key={r.name}
              type="button"
              disabled={claimed}
              onClick={() => onSelect(r.name)}
              className={cn(
                "flex w-full flex-col gap-0.5 border-b px-3 py-2.5 text-left last:border-b-0",
                selectedName === r.name && "bg-accent",
                claimed
                  ? "cursor-not-allowed opacity-60"
                  : "hover:bg-accent/50",
              )}
            >
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{r.name}</span>
                {claimed ? (
                  <Badge variant="secondary">已添加</Badge>
                ) : (
                  <Badge variant="outline">读卡器</Badge>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                {claimed
                  ? `已作为设备 ${r.claimed_id} 接入`
                  : "添加后可在设备详情写 profile，不要和模组同时占同一张卡"}
              </p>
            </button>
          );
        })}
      </div>
    </div>
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
