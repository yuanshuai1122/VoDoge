"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, Check, Pencil, Trash2, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import { EsimDownloadDialog } from "./esim-download-dialog";
import { EsimNotifications, EsimChipInfo } from "./esim-notifications";
import {
  listProfiles,
  switchProfile,
  renameProfile,
  deleteProfile,
  type DeleteProfileResult,
} from "@/lib/api/endpoints/esim";
import { useEsimMutation } from "@/hooks/use-esim-mutation";
import { useEsimLock } from "@/stores/esim-lock";
import { isProfileEnabled, type ProfileItem } from "@/types/esim";
import { Sensitive } from "@/components/common/sensitive";

export function EsimTab({ deviceId }: { deviceId: string }) {
  const [downloadOpen, setDownloadOpen] = useState(false);
  const lock = useEsimLock(deviceId);

  const query = useQuery({
    queryKey: ["esim", deviceId, "profiles"],
    queryFn: () => listProfiles(deviceId),
    // eSIM 读取同样走仲裁器，轮询会加剧争抢，改为手动刷新
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  });

  const doSwitch = useEsimMutation({
    deviceId,
    operation: "switch_profile",
    mutationFn: (args: { iccid: string; aid_hex?: string }) =>
      switchProfile(deviceId, args),
    successMessage: "已切换 Profile",
  });

  const doRename = useEsimMutation({
    deviceId,
    operation: "rename_profile",
    mutationFn: (args: { iccid: string; name: string; aid_hex?: string }) =>
      renameProfile(deviceId, args.iccid, {
        name: args.name,
        aid_hex: args.aid_hex,
      }),
    successMessage: "已重命名",
  });

  const doDelete = useEsimMutation<{ iccid: string }, DeleteProfileResult>({
    deviceId,
    operation: "delete_profile",
    mutationFn: (args) => deleteProfile(deviceId, args.iccid),
    // 删除可能带 warning（如空间回收异常），不能吞掉
    successMessage: (r) =>
      r?.warning ? `Profile 已删除（${r.warning}）` : "Profile 已删除",
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            disabled={lock.locked}
            onClick={() => setDownloadOpen(true)}
          >
            <Download className="size-4" />
            下载 Profile
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={lock.locked || query.isFetching}
            onClick={() => query.refetch()}
          >
            {query.isFetching ? (
              <Loader2 className="size-4 animate-spin" />
            ) : null}
            刷新
          </Button>
        </div>

        {lock.locked && (
          <Badge variant="secondary">
            {lock.running
              ? "操作进行中…"
              : `eSIM 忙碌，${Math.ceil(lock.remainingMs / 1000)} 秒后可重试`}
          </Badge>
        )}
      </div>

      {lock.remainingMs > 0 && lock.reason && (
        <Alert>
          <AlertDescription>
            eSIM 通道被占用（{lock.reason}）。占用方可能是其它客户端或后台任务。
          </AlertDescription>
        </Alert>
      )}

      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => query.refetch()} />
      ) : query.isPending ? (
        <Skeleton className="h-64" />
      ) : query.data.length === 0 ? (
        <EmptyState
          title="未检测到 eUICC"
          description="设备可能不支持 eSIM，或 eSIM 通道未就绪。"
        />
      ) : (
        query.data.map((group) => (
          <Card key={`${group.eid}:${group.aid_hex}`}>
            <CardHeader>
              <CardTitle className="text-base">
                eUICC
                <span className="ml-2 font-mono text-xs font-normal text-muted-foreground">
                  EID <Sensitive value={group.eid} visible={6} />
                </span>
              </CardTitle>
            </CardHeader>

            <CardContent>
              {group.profiles.length === 0 ? (
                <p className="text-sm text-muted-foreground">该 eUICC 暂无 Profile。</p>
              ) : (
                <div className="flex flex-col divide-y">
                  {group.profiles.map((p) => (
                    <ProfileRow
                      key={p.iccid}
                      profile={p}
                      locked={lock.locked}
                      onSwitch={() =>
                        doSwitch.mutate({ iccid: p.iccid, aid_hex: group.aid_hex })
                      }
                      onRename={(name) =>
                        doRename.mutate({
                          iccid: p.iccid,
                          name,
                          aid_hex: group.aid_hex,
                        })
                      }
                      onDelete={() => doDelete.mutate({ iccid: p.iccid })}
                    />
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        ))
      )}

      <EsimNotifications deviceId={deviceId} />
      <EsimChipInfo deviceId={deviceId} />

      <EsimDownloadDialog
        deviceId={deviceId}
        open={downloadOpen}
        onOpenChange={setDownloadOpen}
        disabled={lock.remainingMs > 0}
      />
    </div>
  );
}

function ProfileRow({
  profile,
  locked,
  onSwitch,
  onRename,
  onDelete,
}: {
  profile: ProfileItem;
  locked: boolean;
  onSwitch: () => void;
  onRename: (name: string) => void;
  onDelete: () => void;
}) {
  const enabled = isProfileEnabled(profile);

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 py-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">
            {profile.name || profile.service_provider_name || "未命名 Profile"}
          </span>
          {enabled && (
            <Badge variant="default" className="gap-1">
              <Check className="size-3" />
              使用中
            </Badge>
          )}
        </div>
        <p className="mt-0.5 font-mono text-xs text-muted-foreground">
          <Sensitive value={profile.iccid} />
          {profile.service_provider_name && ` · ${profile.service_provider_name}`}
          {profile.state_text && ` · ${profile.state_text}`}
        </p>
      </div>

      <div className="flex items-center gap-1">
        {!enabled && (
          <Button
            variant="outline"
            size="sm"
            disabled={locked}
            onClick={onSwitch}
          >
            启用
          </Button>
        )}

        <Button
          variant="ghost"
          size="icon"
          aria-label="重命名"
          disabled={locked}
          onClick={() => {
            const name = prompt("输入新的 Profile 名称", profile.name);
            if (name && name.trim()) onRename(name.trim());
          }}
        >
          <Pencil className="size-4" />
        </Button>

        <Button
          variant="ghost"
          size="icon"
          aria-label="删除"
          // 使用中的 Profile 删除会导致断网，禁止直接删
          disabled={locked || enabled}
          title={enabled ? "请先切换到其它 Profile 再删除" : undefined}
          onClick={() => {
            if (
              confirm(
                `确定删除 Profile「${profile.name || profile.iccid}」？删除后无法恢复，需要重新下载。`,
              )
            ) {
              onDelete();
            }
          }}
        >
          <Trash2 className="size-4" />
        </Button>
      </div>
    </div>
  );
}
