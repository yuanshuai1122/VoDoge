"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
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
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  saveProxyConfig,
  type ProxyInstance,
  type ProxyDevice,
} from "@/lib/api/endpoints/proxy";
import { ApiError } from "@/lib/api/errors";

/** 后端对已保存密码的脱敏占位；原样回传时会被还原成真实密码。 */
const MASKED_SECRET = "******";

const MODES = ["socks5", "http"];

/**
 * 新增 / 编辑本机代理实例。
 *
 * 保存接口是 `PUT /proxy-instances/config`，**整体替换**整个实例列表，
 * 因此提交时必须带上所有实例，而不只是被编辑的那一个。
 *
 * 密码：概览接口返回的是 `******`。与上游代理不同，后端在保存时会识别这个
 * 占位并还原原密码（restoreProxySecrets），所以不修改时原样回传即可。
 */
export function InstanceDialog({
  open,
  onOpenChange,
  editing,
  allInstances,
  devices,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** 传入表示编辑，否则为新增 */
  editing?: ProxyInstance | null;
  /** 当前全部实例——整体替换需要 */
  allInstances: ProxyInstance[];
  devices: ProxyDevice[];
}) {
  const isEdit = Boolean(editing);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? "编辑代理实例" : "新增代理实例"}</DialogTitle>
          <DialogDescription>
            实例必须绑定一台设备，出站流量将走该设备的网卡。
          </DialogDescription>
        </DialogHeader>

        <InstanceForm
          key={editing?.id ?? "new"}
          editing={editing ?? null}
          allInstances={allInstances}
          devices={devices}
          onDone={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  );
}

function InstanceForm({
  editing,
  allInstances,
  devices,
  onDone,
}: {
  editing: ProxyInstance | null;
  allInstances: ProxyInstance[];
  devices: ProxyDevice[];
  onDone: () => void;
}) {
  const queryClient = useQueryClient();

  const [draft, setDraft] = useState<ProxyInstance>(
    editing ?? {
      id: "",
      name: "",
      device_id: devices[0]?.id ?? "",
      enabled: true,
      mode: "socks5",
      listen_addr: "0.0.0.0",
      listen_port: 0,
      auth_enabled: false,
      username: "",
      password: "",
    },
  );

  const patch = (v: Partial<ProxyInstance>) =>
    setDraft((prev) => ({ ...prev, ...v }));

  const save = useMutation({
    mutationFn: () => {
      // 整体替换：把被编辑项合并回完整列表再提交
      const next = editing
        ? allInstances.map((i) => (i.id === editing.id ? draft : i))
        : [...allInstances, draft];
      return saveProxyConfig(next);
    },
    onSuccess: () => {
      toast.success(editing ? "已保存" : "已新增");
      queryClient.invalidateQueries({ queryKey: ["proxy"] });
      onDone();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  // 与后端 normalizeProxyInstanceForSave 的校验保持一致，避免提交后才报错
  const idInvalid = !draft.id.trim();
  const deviceInvalid = !draft.device_id.trim();
  const portInvalid = draft.listen_port <= 0 || draft.listen_port > 65535;
  const authInvalid =
    draft.auth_enabled && (!draft.username.trim() || !(draft.password ?? "").trim());
  const duplicateId =
    !editing && allInstances.some((i) => i.id === draft.id.trim());

  const invalid =
    idInvalid || deviceInvalid || portInvalid || authInvalid || duplicateId;

  return (
    <>
      <div className="flex flex-col gap-4">
        <Field id="inst_id" label="实例 ID" error={duplicateId ? "该 ID 已存在" : undefined}>
          <Input
            id="inst_id"
            value={draft.id}
            disabled={Boolean(editing)}
            placeholder="例如 proxy-1"
            onChange={(e) => patch({ id: e.target.value })}
          />
        </Field>

        <Field id="inst_name" label="名称（可选）">
          <Input
            id="inst_name"
            value={draft.name ?? ""}
            onChange={(e) => patch({ name: e.target.value })}
          />
        </Field>

        <Field id="inst_device" label="绑定设备">
          <Select
            value={draft.device_id || ""}
            onValueChange={(v) => v && patch({ device_id: v })}
          >
            <SelectTrigger id="inst_device">
              <SelectValue placeholder="选择设备" />
            </SelectTrigger>
            <SelectContent>
              {devices.map((d) => (
                <SelectItem key={d.id} value={d.id}>
                  {d.name || d.id}
                  {d.interface ? `（${d.interface}）` : ""}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <Field id="inst_mode" label="协议">
          <Select
            value={draft.mode || "socks5"}
            onValueChange={(v) => v && patch({ mode: v })}
          >
            <SelectTrigger id="inst_mode">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {MODES.map((m) => (
                <SelectItem key={m} value={m}>
                  {m}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field id="inst_addr" label="监听地址">
            <Input
              id="inst_addr"
              value={draft.listen_addr ?? ""}
              placeholder="0.0.0.0"
              onChange={(e) => patch({ listen_addr: e.target.value })}
            />
          </Field>

          <Field
            id="inst_port"
            label="端口"
            error={portInvalid ? "需在 1–65535 之间" : undefined}
          >
            <Input
              id="inst_port"
              type="number"
              value={String(draft.listen_port || "")}
              onChange={(e) =>
                patch({ listen_port: Number(e.target.value) || 0 })
              }
            />
          </Field>
        </div>

        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="inst_enabled">启用</Label>
          <Switch
            id="inst_enabled"
            checked={draft.enabled}
            onCheckedChange={(v) => patch({ enabled: v })}
          />
        </div>

        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="inst_auth">需要认证</Label>
          <Switch
            id="inst_auth"
            checked={draft.auth_enabled}
            onCheckedChange={(v) => patch({ auth_enabled: v })}
          />
        </div>

        {draft.auth_enabled && (
          <>
            <Field id="inst_user" label="用户名">
              <Input
                id="inst_user"
                value={draft.username ?? ""}
                onChange={(e) => patch({ username: e.target.value })}
              />
            </Field>

            <Field
              id="inst_pass"
              label="密码"
              hint={
                draft.password === MASKED_SECRET
                  ? "保持不变即沿用原密码；要修改请清空后重新输入"
                  : undefined
              }
            >
              <Input
                id="inst_pass"
                type={draft.password === MASKED_SECRET ? "text" : "password"}
                value={draft.password ?? ""}
                onChange={(e) => patch({ password: e.target.value })}
              />
            </Field>
          </>
        )}
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onDone}>
          取消
        </Button>
        <Button disabled={save.isPending || invalid} onClick={() => save.mutate()}>
          {save.isPending ? "保存中…" : "保存"}
        </Button>
      </DialogFooter>
    </>
  );
}

function Field({
  id,
  label,
  hint,
  error,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error ? (
        <p className="text-xs text-destructive">{error}</p>
      ) : hint ? (
        <p className="text-xs text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  );
}
