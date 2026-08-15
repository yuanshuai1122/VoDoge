"use client";

import { useCallback, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { RefreshCw } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
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
  reconnectVoWiFi,
} from "@/lib/api/endpoints/devices";
import { useEventSource } from "@/lib/sse/use-event-source";
import { ApiError } from "@/lib/api/errors";
import { Sensitive } from "@/components/common/sensitive";
import { E911Card } from "@/components/devices/e911-card";
import { LaneBadge } from "@/components/common/lane-badge";
import { TrafficChart } from "@/components/traffic/traffic-chart";
import type { DeviceOverview } from "@/types/device";
import { useT } from "@/lib/i18n";

export function DeviceOverviewTab({ deviceId }: { deviceId: string }) {
  const t = useT();
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
    toast.error(e instanceof ApiError ? e.message : t("ov.failed"));

  const network = useMutation({
    mutationFn: (enabled: boolean) => setDeviceNetwork(deviceId, { enabled }),
    onSuccess: () => {
      toast.success(t("ov.netSwitched"));
      invalidate();
    },
    onError,
  });

  const vowifi = useMutation({
    mutationFn: (enabled: boolean) => setVoWiFi(deviceId, enabled),
    onSuccess: () => {
      toast.success(t("ov.vowifiSwitched"));
      invalidate();
    },
    onError,
  });

  const flight = useMutation({
    mutationFn: (enabled: boolean) => setFlightMode(deviceId, enabled),
    onSuccess: () => {
      toast.success(t("ov.flightSwitched"));
      invalidate();
    },
    onError,
  });

  const reconnect = useMutation({
    mutationFn: () => reconnectVoWiFi(deviceId),
    onSuccess: () => {
      toast.success(t("ov.imsReReg"));
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
          {status === "open" ? t("ov.live") : status === "error" ? t("ov.streamDown") : t("ov.connecting")}
        </Badge>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("ov.network")}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Row label={t("ov.reg")} value={device.registration_state_label || "-"} />
            <Row label={t("ov.operator")} value={m?.operator || "-"} />
            <Row
              label={t("ov.signal")}
              value={<SignalIndicator rsrp={m?.signal_rsrp} />}
            />
            <Row label={t("ov.mode")} value={m?.network_mode || "-"} />
            <Row label={t("ov.apn")} value={m?.apn || "-"} />
            <Row label={t("ov.publicIp")} value={device.public_ip || "-"} />
            <Row label={t("ov.privateIp")} value={device.private_ip || "-"} />
            <Row label={t("ov.cell")} value={`${m?.cell_id || "-"} / ${m?.lac || "-"}`} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("ov.identity")}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Row
              label={t("ov.lane")}
              value={device.lane ? <LaneBadge lane={device.lane} /> : t("lane.none")}
            />
            <Row label={t("ov.id")} value={<code className="text-xs">{device.id}</code>} />
            <Row label="IMEI" value={<Sensitive value={m?.imei} />} mono />
            <Row label="ICCID" value={<Sensitive value={m?.iccid} />} mono />
            <Row label="IMSI" value={<Sensitive value={m?.imsi} />} mono />
            <Row label={t("ov.phone")} value={device.local_phone || "-"} />
            <Row label={t("ov.fw")} value={m?.firmware || "-"} />
            <Row label={t("ov.backend")} value={device.backend_mode || "-"} />
            <Row
              label={t("ov.esim")}
              value={device.active_esim_profile_name || "-"}
            />
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("ov.toggles")}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Toggle
            id="network"
            label={t("ov.data")}
            description={t("ov.dataHint")}
            checked={device.network_enabled}
            disabled={network.isPending}
            onChange={(v) => network.mutate(v)}
          />
          <Toggle
            id="vowifi"
            label={t("ov.vowifi")}
            description={
              device.vowifi_active ? t("ov.imsOn") : t("ov.imsOff")
            }
            checked={device.vowifi_enabled}
            disabled={vowifi.isPending}
            onChange={(v) => vowifi.mutate(v)}
            action={
              // 仅在已启用时才有重连的意义；注册可能耗时数十秒
              device.vowifi_enabled ? (
                <Button
                  variant="outline"
                  size="xs"
                  disabled={reconnect.isPending || vowifi.isPending}
                  onClick={() => reconnect.mutate()}
                >
                  <RefreshCw
                    className={cn(
                      "size-3.5",
                      reconnect.isPending && "animate-spin",
                    )}
                  />
                  {reconnect.isPending ? t("ov.reReging") : t("ov.reReg")}
                </Button>
              ) : null
            }
          />
          <Toggle
            id="flight"
            label={t("ov.flight")}
            description={t("ov.flightHint")}
            checked={m?.operating_mode === 1}
            disabled={flight.isPending}
            onChange={(v) => flight.mutate(v)}
          />
        </CardContent>
      </Card>

      {/* VoWiFi 未启用时登记紧急地址没有意义——E911 是 VoWiFi 通话的前置条件 */}
      {device.vowifi_enabled && (
        <E911Card
          deviceId={deviceId}
          available={device.e911_setup_available}
        />
      )}

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
  action,
}: {
  id: string;
  label: string;
  description: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
  /** 开关旁的附加操作，如 VoWiFi 的「重新注册」 */
  action?: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div>
        <Label htmlFor={id}>{label}</Label>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <div className="flex items-center gap-2">
        {action}
        <Switch
          id={id}
          checked={checked}
          disabled={disabled}
          onCheckedChange={onChange}
        />
      </div>
    </div>
  );
}
