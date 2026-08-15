"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import {
  createProfileBindings,
  deleteProfileBindings,
  listProfileBindings,
  listUpstreamProxies,
} from "@/lib/api/endpoints/proxy";
import { listDevices } from "@/lib/api/endpoints/devices";
import { listProfiles } from "@/lib/api/endpoints/esim";
import { ApiError } from "@/lib/api/errors";
import { laneLabel } from "@/lib/lane";

export function ProfileBindings() {
  const queryClient = useQueryClient();
  const bindings = useQuery({
    queryKey: ["proxy", "profile-bindings"],
    queryFn: listProfileBindings,
  });
  const proxies = useQuery({
    queryKey: ["proxy", "upstream"],
    queryFn: listUpstreamProxies,
  });
  const devices = useQuery({
    queryKey: ["devices"],
    queryFn: listDevices,
  });

  const [proxyId, setProxyId] = useState("");
  const [deviceId, setDeviceId] = useState("");
  const [iccid, setIccid] = useState("");
  const [profileName, setProfileName] = useState("");

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["proxy", "profile-bindings"] });
  const onError = (e: unknown) =>
    toast.error(e instanceof ApiError ? e.message : "操作失败");

  const save = useMutation({
    mutationFn: () =>
      createProfileBindings({
        upstream_proxy_id: proxyId,
        bindings: [
          { device_id: deviceId, iccid: iccid.trim(), profile_name: profileName },
        ],
      }),
    onSuccess: () => {
      toast.success("已绑定");
      setIccid("");
      setProfileName("");
      invalidate();
    },
    onError,
  });

  const remove = useMutation({
    mutationFn: (row: { iccid: string; upstream_proxy_id: string }) =>
      deleteProfileBindings({
        upstream_proxy_id: row.upstream_proxy_id,
        iccids: [row.iccid],
      }),
    onSuccess: () => {
      toast.success("已解除绑定");
      invalidate();
    },
    onError,
  });

  const loadProfiles = useMutation({
    mutationFn: async (id: string) => {
      const groups = await listProfiles(id);
      return groups.flatMap((g) =>
        (g.profiles ?? []).map((p) => ({
          iccid: p.iccid,
          name: p.name || p.service_provider_name || p.iccid,
        })),
      );
    },
    onError: (e) =>
      toast.error(e instanceof ApiError ? e.message : "读取 Profile 失败"),
  });

  const proxyName = useMemo(() => {
    const m = new Map((proxies.data ?? []).map((p) => [p.id, p.name || p.addr || p.id]));
    return (id: string) => m.get(id) || id;
  }, [proxies.data]);

  if (bindings.isError) {
    return <ErrorState error={bindings.error} onRetry={() => bindings.refetch()} />;
  }
  if (bindings.isPending || proxies.isPending) {
    return <Skeleton className="h-48" />;
  }

  const enabledProxies = (proxies.data ?? []).filter((p) => p.enabled);
  const deviceList = devices.data?.devices ?? [];
  const selectedDevice = deviceList.find((d) => d.id === deviceId);
  const canSubmit =
    proxyId !== "" &&
    deviceId.trim() !== "" &&
    /^\d{18,22}$/.test(iccid.trim()) &&
    !save.isPending;

  return (
    <div className="flex flex-col gap-4">
      <Alert>
        <AlertDescription>
          VoWiFi 按当前 ICCID 选前置代理：国外线 / 未分线时，Profile
          绑定优先于国家规则。国内线始终走模组出口，绑定会保存但不生效。同一 ICCID
          只能绑一个代理。
        </AlertDescription>
      </Alert>

      {(bindings.data ?? []).length === 0 ? (
        <EmptyState
          title="尚未绑定 SIM / Profile"
          description="把一张实体卡或一个 eSIM Profile 绑到指定前置代理。"
        />
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>设备</TableHead>
                <TableHead>ICCID</TableHead>
                <TableHead>SIM / Profile</TableHead>
                <TableHead>前置代理</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(bindings.data ?? []).map((row) => (
                <TableRow key={row.iccid}>
                  <TableCell className="font-mono text-xs">{row.device_id}</TableCell>
                  <TableCell className="font-mono text-xs">{row.iccid}</TableCell>
                  <TableCell>{row.profile_name || row.iccid}</TableCell>
                  <TableCell>{proxyName(row.upstream_proxy_id)}</TableCell>
                  <TableCell>
                    <div className="flex justify-end">
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="解除绑定"
                        disabled={remove.isPending}
                        onClick={() => {
                          if (confirm(`解除 ${row.iccid} 的绑定？`)) {
                            remove.mutate(row);
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

      {enabledProxies.length === 0 ? (
        <Alert>
          <AlertDescription>
            请先新增并启用一个上游代理，才能绑定 Profile。
          </AlertDescription>
        </Alert>
      ) : (
        <div className="flex flex-col gap-3 rounded-lg border p-3">
          <p className="text-sm font-medium">添加绑定</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label>前置代理</Label>
              <Select value={proxyId} onValueChange={(v) => v && setProxyId(v)}>
                <SelectTrigger>
                  <SelectValue placeholder="选择上游代理" />
                </SelectTrigger>
                <SelectContent>
                  {enabledProxies.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name || p.addr}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label>设备</Label>
              <Select
                value={deviceId}
                onValueChange={(v) => {
                  if (!v) return;
                  setDeviceId(v);
                  const d = deviceList.find((x) => x.id === v);
                  const live = d?.modem?.iccid?.trim() ?? "";
                  if (live) setIccid(live);
                }}
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择设备" />
                </SelectTrigger>
                <SelectContent>
                  {deviceList.map((d) => (
                    <SelectItem key={d.id} value={d.id}>
                      {d.name || d.id}
                      {d.lane ? ` · ${laneLabel(d.lane)}` : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="binding_iccid">ICCID（18–22 位数字）</Label>
              <Input
                id="binding_iccid"
                value={iccid}
                onChange={(e) => setIccid(e.target.value)}
                className="font-mono"
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="binding_name">显示名（可选）</Label>
              <Input
                id="binding_name"
                value={profileName}
                onChange={(e) => setProfileName(e.target.value)}
              />
            </div>
          </div>
          {selectedDevice ? (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={loadProfiles.isPending}
                onClick={() => loadProfiles.mutate(selectedDevice.id)}
              >
                {loadProfiles.isPending ? "读取中…" : "读取该设备 Profile"}
              </Button>
              {(loadProfiles.data ?? []).map((p) => (
                <Button
                  key={p.iccid}
                  type="button"
                  variant="outline"
                  size="sm"
                  className="font-mono text-xs"
                  onClick={() => {
                    setIccid(p.iccid);
                    setProfileName(p.name);
                  }}
                >
                  {p.name}
                </Button>
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              设备离线也可手填 ICCID。读卡器设备与模组一样按 ICCID 绑定。
            </p>
          )}
          <Button
            type="button"
            className="self-start"
            disabled={!canSubmit}
            onClick={() => save.mutate()}
          >
            {save.isPending ? "保存中…" : "保存绑定"}
          </Button>
        </div>
      )}
    </div>
  );
}
