"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
  disableProfile,
  renameProfile,
  deleteProfile,
  type DeleteProfileResult,
  type SwitchProfileResult,
} from "@/lib/api/endpoints/esim";
import { useEsimMutation } from "@/hooks/use-esim-mutation";
import { useEsimLock } from "@/stores/esim-lock";
import {
  currentProfile,
  isProfileEnabled,
  type ProfileItem,
} from "@/types/esim";
import { Sensitive } from "@/components/common/sensitive";

export function EsimTab({ deviceId }: { deviceId: string }) {
  const [downloadOpen, setDownloadOpen] = useState(false);
  const lock = useEsimLock(deviceId);
  const queryClient = useQueryClient();

  const afterIdentityChange = () => {
    // 切卡后短信身份跟新 ICCID，设备和会话列表都要重拉
    queryClient.invalidateQueries({ queryKey: ["devices"] });
    queryClient.invalidateQueries({ queryKey: ["sms"] });
  };

  const query = useQuery({
    queryKey: ["esim", deviceId, "profiles"],
    queryFn: () => listProfiles(deviceId),
    // eSIM 读取同样走仲裁器，轮询会加剧争抢，改为手动刷新
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  });

  const doSwitch = useEsimMutation<
    { iccid: string; aid_hex?: string },
    SwitchProfileResult
  >({
    deviceId,
    operation: "switch_profile",
    mutationFn: (args) => switchProfile(deviceId, args),
    successMessage: (r) => {
      if (r?.degraded_reason) return `已切换，但设备降级：${r.degraded_reason}`;
      if (r?.sim_reload_warning) return `已切换（${r.sim_reload_warning}）`;
      if (r?.recovery_pending) return "已切换，正在恢复射频，短信身份稍后更新";
      return "已切换 Profile。短信将跟这张卡走。";
    },
    onSuccess: afterIdentityChange,
  });

  const doDisable = useEsimMutation({
    deviceId,
    operation: "disable_profile",
    mutationFn: (args: { iccid: string; aid_hex?: string }) =>
      disableProfile(deviceId, args),
    successMessage: (r) =>
      r?.message || "已禁用 Profile。该卡槽现在没有活动号码。",
    onSuccess: afterIdentityChange,
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

  const doDelete = useEsimMutation<
    { iccid: string; aid_hex?: string },
    DeleteProfileResult
  >({
    deviceId,
    operation: "delete_profile",
    mutationFn: (args) => deleteProfile(deviceId, args.iccid, args.aid_hex),
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
        <>
          <CurrentProfileCard
            current={currentProfile(query.data)}
            locked={lock.locked}
            onDisable={(iccid, aidHex) => {
              if (
                confirm(
                  `确定禁用当前 Profile（${iccid}）吗？禁用后此卡槽没有活动号码，短信会停到重新启用一张为止。`,
                )
              ) {
                doDisable.mutate({ iccid, aid_hex: aidHex });
              }
            }}
          />
          {query.data.map((group) => (
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
                  <p className="text-sm text-muted-foreground">
                    该 eUICC 暂无 Profile。
                  </p>
                ) : (
                  <div className="flex flex-col divide-y">
                    {group.profiles.map((p) => (
                      <ProfileRow
                        key={p.iccid}
                        profile={p}
                        locked={lock.locked}
                        onSwitch={() => {
                          if (
                            confirm(
                              `确定启用此 Profile（${p.iccid}）吗？切换后设备会短暂断网，短信将改跟这张卡走。`,
                            )
                          ) {
                            doSwitch.mutate({
                              iccid: p.iccid,
                              aid_hex: group.aid_hex,
                            });
                          }
                        }}
                        onDisable={() => {
                          if (
                            confirm(
                              `确定禁用当前 Profile（${p.iccid}）吗？禁用后此卡槽没有活动号码，短信会停到重新启用一张为止。`,
                            )
                          ) {
                            doDisable.mutate({
                              iccid: p.iccid,
                              aid_hex: group.aid_hex,
                            });
                          }
                        }}
                        onRename={(name) =>
                          doRename.mutate({
                            iccid: p.iccid,
                            name,
                            aid_hex: group.aid_hex,
                          })
                        }
                        onDelete={() =>
                          doDelete.mutate({
                            iccid: p.iccid,
                            aid_hex: group.aid_hex,
                          })
                        }
                      />
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          ))}
        </>
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

function CurrentProfileCard({
  current,
  locked,
  onDisable,
}: {
  current: ReturnType<typeof currentProfile>;
  locked: boolean;
  onDisable: (iccid: string, aidHex: string) => void;
}) {
  if (!current) {
    return (
      <Alert>
        <AlertDescription>
          当前没有启用的 Profile。短信不会发出，直到启用一张卡。
        </AlertDescription>
      </Alert>
    );
  }
  const label =
    current.profile.name ||
    current.profile.service_provider_name ||
    "未命名 Profile";
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">当前号码</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{label}</p>
          <p className="mt-0.5 font-mono text-xs text-muted-foreground">
            <Sensitive value={current.profile.iccid} />
            {current.profile.service_provider_name &&
              ` · ${current.profile.service_provider_name}`}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            短信按这张卡的 ICCID 落库。切到另一张后，新短信走新身份，旧会话还在。
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={locked}
          onClick={() => onDisable(current.profile.iccid, current.aid_hex)}
        >
          禁用当前卡
        </Button>
      </CardContent>
    </Card>
  );
}

function ProfileRow({
  profile,
  locked,
  onSwitch,
  onDisable,
  onRename,
  onDelete,
}: {
  profile: ProfileItem;
  locked: boolean;
  onSwitch: () => void;
  onDisable: () => void;
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
        {enabled ? (
          <Button
            variant="outline"
            size="sm"
            disabled={locked}
            onClick={onDisable}
          >
            禁用
          </Button>
        ) : (
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
