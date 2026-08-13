"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
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
import { ErrorState } from "@/components/common/empty-state";
import { Sensitive } from "@/components/common/sensitive";
import {
  getDeviceConfig,
  setUSBNetMode,
  updateDevice,
  USBNET_MODES,
} from "@/lib/api/endpoints/devices";
import { ApiError } from "@/lib/api/errors";
import type { DeviceConfigDTO } from "@/types/device-config";

const ESIM_TRANSPORTS = ["", "at", "qmi", "mbim"];
const BACKENDS = ["", "qmi", "mbim", "at"];

/**
 * 设备配置。
 *
 * 只包含硬件与身份字段。网络/VoWiFi/APN/IP 版本属于**卡策略**，
 * 后端在保存设备时会用当前有效策略覆盖请求体中的同名字段
 * （GET config 也不投影它们），请改用「卡策略」页签编辑。
 */
export function ConfigTab({ deviceId }: { deviceId: string }) {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ["devices", "config", deviceId],
    queryFn: () => getDeviceConfig(deviceId),
  });

  const save = useMutation({
    mutationFn: (cfg: DeviceConfigDTO) => updateDevice(deviceId, cfg),
    onSuccess: (r) => {
      if (r?.warning) toast.warning(r.warning);
      else if (r?.requires_restart) toast.success("已保存，设备将重启以生效");
      else toast.success("配置已保存");
      queryClient.invalidateQueries({ queryKey: ["devices"] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }
  if (query.isPending) return <Skeleton className="h-64" />;

  return (
    <ConfigForm
      // 换设备即重建表单，避免把上一台的草稿带过来
      key={deviceId}
      deviceId={deviceId}
      initial={query.data}
      saving={save.isPending}
      onSave={(v) => save.mutate(v)}
    />
  );
}

function ConfigForm({
  deviceId,
  initial,
  saving,
  onSave,
}: {
  deviceId: string;
  initial: DeviceConfigDTO;
  saving: boolean;
  onSave: (v: DeviceConfigDTO) => void;
}) {
  // 保存是整体替换，因此草稿必须从完整配置起步，未编辑的字段也要原样回传
  const [draft, setDraft] = useState<DeviceConfigDTO>(initial);

  const patch = (v: Partial<DeviceConfigDTO>) =>
    setDraft((prev) => ({ ...prev, ...v }));

  return (
    <div className="flex max-w-2xl flex-col gap-5">
      <Alert>
        <AlertDescription>
          此处只保存硬件与身份字段。数据网络、VoWiFi、APN、IP 版本属于卡策略，
          请在「卡策略」页签编辑——在这里改动不会生效。
        </AlertDescription>
      </Alert>

      <Field id="name" label="设备名称" hint="留空则显示设备 ID">
        <Input
          id="name"
          value={draft.name ?? ""}
          onChange={(e) => patch({ name: e.target.value })}
        />
      </Field>

      <Field
        id="proxy_port"
        label="代理端口"
        hint="0 表示不为该设备单独监听端口"
      >
        <Input
          id="proxy_port"
          type="number"
          value={String(draft.proxy_port ?? 0)}
          onChange={(e) => patch({ proxy_port: Number(e.target.value) || 0 })}
        />
      </Field>

      <Field id="esim_transport" label="eSIM 通道">
        <Select
          value={draft.esim_transport || ""}
          onValueChange={(v) => patch({ esim_transport: v ?? "" })}
        >
          <SelectTrigger id="esim_transport">
            <SelectValue placeholder="自动" />
          </SelectTrigger>
          <SelectContent>
            {ESIM_TRANSPORTS.map((v) => (
              <SelectItem key={v || "auto"} value={v}>
                {v || "自动"}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <Field
        id="device_backend"
        label="接入后端"
        hint="改动会导致设备重建连接"
      >
        <Select
          value={draft.device_backend || ""}
          onValueChange={(v) => patch({ device_backend: v ?? "" })}
        >
          <SelectTrigger id="device_backend">
            <SelectValue placeholder="自动" />
          </SelectTrigger>
          <SelectContent>
            {BACKENDS.map((v) => (
              <SelectItem key={v || "auto"} value={v}>
                {v || "自动"}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <Field id="module_vendor" label="模组厂商" hint="留空自动识别">
        <Input
          id="module_vendor"
          value={draft.module_vendor ?? ""}
          onChange={(e) => patch({ module_vendor: e.target.value })}
        />
      </Field>

      <USBNetModeCard deviceId={deviceId} />

      {/* 硬件路径由运行时按 IMEI 发现并回写，手改容易绑到错误的物理设备 */}
      <div className="rounded-lg border p-3">
        <p className="mb-2 text-sm font-medium">运行时识别（只读）</p>
        <dl className="grid gap-x-6 gap-y-1.5 sm:grid-cols-2">
          <ReadOnly label="设备 ID" value={draft.id} mono />
          <ReadOnly label="IMEI" value={<Sensitive value={draft.modem_imei} />} />
          <ReadOnly label="网卡" value={draft.interface || "-"} mono />
          <ReadOnly label="控制节点" value={draft.control_device || "-"} mono />
          <ReadOnly label="AT 端口" value={draft.at_port || "-"} mono />
          <ReadOnly label="USB 路径" value={draft.usb_path || "-"} mono />
        </dl>
        <p className="mt-2 text-xs text-muted-foreground">
          这些字段由运行时按 IMEI 发现并回写，不在此处编辑。
        </p>
      </div>

      <Button
        className="self-start"
        disabled={saving}
        onClick={() => onSave(draft)}
      >
        {saving ? "保存中…" : "保存配置"}
      </Button>
    </div>
  );
}

function Field({
  id,
  label,
  hint,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

function ReadOnly({
  label,
  value,
  mono,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className={mono ? "truncate font-mono text-xs" : "truncate text-sm"}>
        {value}
      </dd>
    </div>
  );
}

/**
 * USBNET 模式切换。
 *
 * 独立于「保存配置」，因为它不是配置项而是一次性动作：后端下发
 * `AT+QCFG="usbnet",N` 后立刻 `AT+CFUN=1,1` 重启模组，重启后由不同的内核驱动
 * 接管，控制节点与网卡名都会变。设备会先掉线，再以新形态被重新发现。
 *
 * 选错模式的代价是设备可能不再以预期方式出现（例如切到 RNDIS 后 Linux 上
 * 拿不到 QMI 控制节点），所以这里要求二次确认，且不做乐观更新。
 */
function USBNetModeCard({ deviceId }: { deviceId: string }) {
  const [mode, setMode] = useState<string>("");
  const [confirming, setConfirming] = useState(false);

  const apply = useMutation({
    mutationFn: (m: number) => setUSBNetMode(deviceId, m),
    onSuccess: () => {
      setConfirming(false);
      setMode("");
      toast.success("指令已下发，模组正在重启；请稍候重新扫描设备");
    },
    onError: (e) => {
      setConfirming(false);
      toast.error(e instanceof ApiError ? e.message : "设置失败");
    },
  });

  const selected = USBNET_MODES.find((m) => String(m.value) === mode);

  return (
    <div className="rounded-lg border p-3">
      <p className="mb-1 text-sm font-medium">USBNET 模式</p>
      <p className="mb-3 text-xs text-muted-foreground">
        决定模组以哪种网络设备形态挂到主机。仅 Quectel 模组支持，且需要可用的
        AT 端口；纯 QMI 接管的设备会被拒绝。
      </p>

      <div className="flex flex-wrap items-center gap-2">
        <Select value={mode} onValueChange={(v) => setMode(v ?? "")}>
          <SelectTrigger id="usbnet_mode" className="w-56">
            {/* 值是数字（AT+QCFG 的入参），直接显示会是个光秃秃的 "2"，需映射回名字 */}
            <SelectValue placeholder="选择目标模式">
              {(v: string | null) =>
                USBNET_MODES.find((m) => String(m.value) === v)?.label ??
                "选择目标模式"
              }
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {USBNET_MODES.map((m) => (
              <SelectItem key={m.value} value={String(m.value)}>
                {m.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {!confirming ? (
          <Button
            size="sm"
            variant="outline"
            disabled={!selected || apply.isPending}
            onClick={() => setConfirming(true)}
          >
            切换模式
          </Button>
        ) : (
          <>
            <Button
              size="sm"
              variant="destructive"
              disabled={apply.isPending}
              onClick={() => selected && apply.mutate(selected.value)}
            >
              {apply.isPending ? "下发中…" : "确认切换并重启模组"}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={apply.isPending}
              onClick={() => setConfirming(false)}
            >
              取消
            </Button>
          </>
        )}
      </div>

      {selected && (
        <p className="mt-2 text-xs text-muted-foreground">{selected.hint}</p>
      )}

      {confirming && (
        <Alert variant="destructive" className="mt-3">
          <AlertDescription>
            切到「{selected?.label}」后模组会<strong>立即重启</strong>
            ，控制节点与网卡名都会变，设备将短暂离线。若目标模式不被当前系统驱动
            支持，设备可能不再以预期方式出现，需要物理接触才能改回。
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}
