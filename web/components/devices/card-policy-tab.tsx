"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import { getDeviceOverview } from "@/lib/api/endpoints/devices";
import {
  getCardPolicy,
  putCardPolicy,
  type CardPolicy,
} from "@/lib/api/endpoints/card-policy";
import { ApiError } from "@/lib/api/errors";
import { maskIdentifier } from "@/lib/format";

const IP_VERSIONS = ["", "ipv4", "ipv6", "ipv4v6"];

/**
 * 卡策略按 ICCID 存储，与设备解耦：换卡后策略跟着卡走。
 * 因此这里先从设备概览取出当前 ICCID，再按它读写策略。
 */
export function CardPolicyTab({ deviceId }: { deviceId: string }) {
  const queryClient = useQueryClient();

  const deviceQuery = useQuery({
    queryKey: ["devices", "overview", deviceId],
    queryFn: () => getDeviceOverview(deviceId),
  });

  const iccid = deviceQuery.data?.modem?.iccid ?? "";

  const policyQuery = useQuery({
    queryKey: ["card-policy", iccid],
    queryFn: () => getCardPolicy(iccid),
    enabled: Boolean(iccid),
  });

  const save = useMutation({
    mutationFn: (input: CardPolicy) => putCardPolicy(iccid, input),
    onSuccess: () => {
      toast.success("策略已保存");
      queryClient.invalidateQueries({ queryKey: ["card-policy", iccid] });
      queryClient.invalidateQueries({ queryKey: ["devices"] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  if (deviceQuery.isPending) return <Skeleton className="h-64" />;

  if (!iccid) {
    return (
      <EmptyState
        title="未读取到 ICCID"
        description="设备可能未插卡或尚未就绪，无法定位对应的卡策略。"
      />
    );
  }

  if (policyQuery.isError) {
    return (
      <ErrorState
        error={policyQuery.error}
        onRetry={() => policyQuery.refetch()}
      />
    );
  }

  if (policyQuery.isPending) return <Skeleton className="h-64" />;

  return (
    <PolicyForm
      // 换卡即重建表单，避免把上一张卡的草稿带过来
      key={iccid}
      iccid={iccid}
      initial={policyQuery.data}
      saving={save.isPending}
      onSave={(v) => save.mutate(v)}
    />
  );
}

function PolicyForm({
  iccid,
  initial,
  saving,
  onSave,
}: {
  iccid: string;
  initial: CardPolicy;
  saving: boolean;
  onSave: (v: CardPolicy) => void;
}) {
  const [draft, setDraft] = useState<CardPolicy>(initial);

  return (
    <div className="flex max-w-xl flex-col gap-5">
      <div className="flex items-center gap-2">
        <span className="text-sm text-muted-foreground">ICCID</span>
        <code className="text-xs">{maskIdentifier(iccid)}</code>
        {draft.source && (
          <Badge variant="outline">
            {draft.source === "user" ? "用户设置" : "系统推导"}
          </Badge>
        )}
      </div>

      <Alert>
        <AlertDescription>
          策略绑定 ICCID 而非设备。更换 eSIM Profile 或换卡后，这里的设置不会跟随原设备。
        </AlertDescription>
      </Alert>

      <ToggleRow
        id="network_enabled"
        label="启用数据网络"
        checked={draft.network_enabled}
        onChange={(v) => setDraft({ ...draft, network_enabled: v })}
      />
      <ToggleRow
        id="vowifi_enabled"
        label="启用 VoWiFi"
        checked={draft.vowifi_enabled}
        onChange={(v) => setDraft({ ...draft, vowifi_enabled: v })}
      />
      <ToggleRow
        id="airplane_enabled"
        label="飞行模式"
        checked={draft.airplane_enabled}
        onChange={(v) => setDraft({ ...draft, airplane_enabled: v })}
      />

      <div className="flex flex-col gap-2">
        <Label htmlFor="ip_version">IP 版本</Label>
        <Select
          value={draft.ip_version || ""}
          onValueChange={(v) => setDraft({ ...draft, ip_version: v ?? "" })}
        >
          <SelectTrigger id="ip_version">
            <SelectValue placeholder="默认" />
          </SelectTrigger>
          <SelectContent>
            {IP_VERSIONS.map((v) => (
              <SelectItem key={v || "default"} value={v}>
                {v || "默认"}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="apn">APN</Label>
        <Input
          id="apn"
          value={draft.apn ?? ""}
          placeholder="留空则使用运营商默认值"
          onChange={(e) => setDraft({ ...draft, apn: e.target.value })}
        />
        <p className="text-xs text-muted-foreground">
          APN 与 IP 版本在下次建立数据连接时生效。
        </p>
      </div>

      <Button
        className="self-start"
        disabled={saving}
        onClick={() => onSave(draft)}
      >
        {saving ? "保存中…" : "保存策略"}
      </Button>
    </div>
  );
}

function ToggleRow({
  id,
  label,
  checked,
  onChange,
}: {
  id: string;
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <Label htmlFor={id}>{label}</Label>
      <Switch id={id} checked={checked} onCheckedChange={onChange} />
    </div>
  );
}
