"use client";

import { useCallback, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/common/empty-state";
import {
  DeviceStatusBadge,
  SignalIndicator,
} from "@/components/devices/device-status-badge";
import {
  getDeviceOverview,
  setDeviceNetwork,
  setVoWiFi,
  setFlightMode,
} from "@/lib/api/endpoints/devices";
import { useEventSource } from "@/lib/sse/use-event-source";
import { ApiError } from "@/lib/api/errors";
import { maskIdentifier } from "@/lib/format";
import { TrafficChart } from "@/components/traffic/traffic-chart";
import type { DeviceOverview } from "@/types/device";

export function DeviceOverviewTab({ deviceId }: { deviceId: string }) {
  const queryClient = useQueryClient();
  const queryKey = ["devices", "overview", deviceId] as const;
  const [streamed, setStreamed] = useState<DeviceOverview | null>(null);

  // 首屏用一次性请求，之后由 SSE 持续覆盖
  const query = useQuery({
    queryKey,
    queryFn: () => getDeviceOverview(deviceId),
  });

  const onOverview = useCallback((data: unknown) => {
    // 概览流的 payload 与 REST 一致：{devices:[单元素]}
    const list = (data as { devices?: DeviceOverview[] })?.devices;
    if (Array.isArray(list) && list.length > 0) setStreamed(list[0]);
  }, []);

  const status = useEventSource(`/devices/${encodeURIComponent(deviceId)}/overview/stream`, {
    events: { overview: onOverview },
  });

  const device = streamed ?? query.data;

  const invalidate = () => queryClient.invalidateQueries({ queryKey });
  const onError = (e: unknown) =>
    toast.error(e instanceof ApiError ? e.message : "操作失败");

  const network = useMutation({
    mutationFn: (enabled: boolean) => setDeviceNetwork(deviceId, { enabled }),
    onSuccess: () => {
      toast.success("已提交网络切换");
      invalidate();
    },
    onError,
  });

  const vowifi = useMutation({
    mutationFn: (enabled: boolean) => setVoWiFi(deviceId, enabled),
    onSuccess: () => {
      toast.success("已提交 VoWiFi 切换");
      invalidate();
    },
    onError,
  });

  const flight = useMutation({
    mutationFn: (enabled: boolean) => setFlightMode(deviceId, enabled),
    onSuccess: () => {
      toast.success("已提交飞行模式切换");
      invalidate();
    },
    onError,
  });

  if (query.isError && !device) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }

  if (!device) {
    return (
      <div className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-48" />
        <Skeleton className="h-48" />
      </div>
    );
  }

  const m = device.modem;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <DeviceStatusBadge device={device} showDetail />
        <Badge variant={status === "open" ? "secondary" : "outline"}>
          {status === "open" ? "实时" : status === "error" ? "流中断" : "连接中"}
        </Badge>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">网络</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Row label="注册状态" value={device.registration_state_label || "-"} />
            <Row label="运营商" value={m?.operator || "-"} />
            <Row
              label="信号"
              value={<SignalIndicator rsrp={m?.signal_rsrp} />}
            />
            <Row label="网络制式" value={m?.network_mode || "-"} />
            <Row label="APN" value={m?.apn || "-"} />
            <Row label="出口 IP" value={device.public_ip || "-"} />
            <Row label="内网 IP" value={device.private_ip || "-"} />
            <Row label="小区 / LAC" value={`${m?.cell_id || "-"} / ${m?.lac || "-"}`} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">标识</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Row label="设备 ID" value={<code className="text-xs">{device.id}</code>} />
            <Row label="IMEI" value={maskIdentifier(m?.imei)} mono />
            <Row label="ICCID" value={maskIdentifier(m?.iccid)} mono />
            <Row label="IMSI" value={maskIdentifier(m?.imsi)} mono />
            <Row label="本机号码" value={device.local_phone || "-"} />
            <Row label="固件" value={m?.firmware || "-"} />
            <Row label="接入方式" value={device.backend_mode || "-"} />
            <Row
              label="当前 eSIM"
              value={device.active_esim_profile_name || "-"}
            />
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">开关</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Toggle
            id="network"
            label="数据网络"
            description="关闭后设备不再建立数据连接"
            checked={device.network_enabled}
            disabled={network.isPending}
            onChange={(v) => network.mutate(v)}
          />
          <Toggle
            id="vowifi"
            label="VoWiFi"
            description={
              device.vowifi_active ? "IMS 已注册" : "未注册或未启用"
            }
            checked={device.vowifi_enabled}
            disabled={vowifi.isPending}
            onChange={(v) => vowifi.mutate(v)}
          />
          <Toggle
            id="flight"
            label="飞行模式"
            description="开启后模组停止射频"
            checked={m?.operating_mode === 1}
            disabled={flight.isPending}
            onChange={(v) => flight.mutate(v)}
          />
        </CardContent>
      </Card>

      <TrafficChart deviceId={deviceId} />

      {device.traffic && Object.keys(device.traffic).length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">流量</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {Object.entries(device.traffic).map(([k, v]) => (
              <div key={k}>
                <p className="text-xs text-muted-foreground">{k}</p>
                <p className="text-sm font-medium tabular-nums">{v}</p>
              </div>
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function Row({
  label,
  value,
  mono,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <span className="shrink-0 text-sm text-muted-foreground">{label}</span>
      <span className={mono ? "font-mono text-xs" : "text-sm"}>{value}</span>
    </div>
  );
}

function Toggle({
  id,
  label,
  description,
  checked,
  disabled,
  onChange,
}: {
  id: string;
  label: string;
  description: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div>
        <Label htmlFor={id}>{label}</Label>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <Switch
        id={id}
        checked={checked}
        disabled={disabled}
        onCheckedChange={onChange}
      />
    </div>
  );
}
