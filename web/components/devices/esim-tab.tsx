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
import { useT } from "@/lib/i18n";

export function EsimTab({ deviceId }: { deviceId: string }) {
  const [downloadOpen, setDownloadOpen] = useState(false);
  const lock = useEsimLock(deviceId);
  const queryClient = useQueryClient();
  const t = useT();

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
      if (r?.degraded_reason) return t("esim.switchedDegraded", { reason: r.degraded_reason });
      if (r?.sim_reload_warning) return t("esim.switchedWarn", { warning: r.sim_reload_warning });
      if (r?.recovery_pending) return t("esim.switchedRecover");
      return t("esim.switched");
    },
    onSuccess: afterIdentityChange,
  });

  const doDisable = useEsimMutation({
    deviceId,
    operation: "disable_profile",
    mutationFn: (args: { iccid: string; aid_hex?: string }) =>
      disableProfile(deviceId, args),
    successMessage: (r) =>
      r?.message || t("esim.disabled"),
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
    successMessage: t("esim.renamed"),
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
      r?.warning ? t("esim.deletedWarn", { warning: r.warning }) : t("esim.deleted"),
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
            {t("esim.download")}
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
            {t("common.refresh")}
          </Button>
        </div>

        {lock.locked && (
          <Badge variant="secondary">
            {lock.running
              ? t("esim.running")
              : t("esim.busy", { seconds: Math.ceil(lock.remainingMs / 1000) })}
          </Badge>
        )}
      </div>

      {lock.remainingMs > 0 && lock.reason && (
        <Alert>
          <AlertDescription>
            {t("esim.held", { reason: lock.reason ?? "" })}
          </AlertDescription>
        </Alert>
      )}

      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => query.refetch()} />
      ) : query.isPending ? (
        <Skeleton className="h-64" />
      ) : query.data.length === 0 ? (
        <EmptyState
          title={t("esim.noEuicc")}
          description={t("esim.noEuiccHint")}
        />
      ) : (
        <>
          <CurrentProfileCard
            current={currentProfile(query.data)}
            locked={lock.locked}
            onDisable={(iccid, aidHex) => {
              if (
                confirm(
                  t("esim.confirmDisable", { iccid }),
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
                    {t("esim.noProfiles")}
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
                              t("esim.confirmEnable", { iccid: p.iccid }),
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
                              t("esim.confirmDisable", { iccid: p.iccid }),
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
  const t = useT();
  if (!current) {
    return (
      <Alert>
        <AlertDescription>
          {t("esim.noActive")}
        </AlertDescription>
      </Alert>
    );
  }
  const label =
    current.profile.name ||
    current.profile.service_provider_name ||
    t("esim.unnamed");
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("esim.current")}</CardTitle>
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
            {t("esim.currentHint")}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={locked}
          onClick={() => onDisable(current.profile.iccid, current.aid_hex)}
        >
          {t("esim.disableCurrent")}
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
  const t = useT();
  const enabled = isProfileEnabled(profile);

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 py-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">
            {profile.name || profile.service_provider_name || t("esim.unnamed")}
          </span>
          {enabled && (
            <Badge variant="default" className="gap-1">
              <Check className="size-3" />
              {t("esim.inUse")}
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
            {t("esim.disable")}
          </Button>
        ) : (
          <Button
            variant="outline"
            size="sm"
            disabled={locked}
            onClick={onSwitch}
          >
            {t("esim.enable")}
          </Button>
        )}

        <Button
          variant="ghost"
          size="icon"
          aria-label={t("esim.rename")}
          disabled={locked}
          onClick={() => {
            const name = prompt(t("esim.renamePrompt"), profile.name);
            if (name && name.trim()) onRename(name.trim());
          }}
        >
          <Pencil className="size-4" />
        </Button>

        <Button
          variant="ghost"
          size="icon"
          aria-label={t("common.delete")}
          // 使用中的 Profile 删除会导致断网，禁止直接删
          disabled={locked || enabled}
          title={enabled ? t("esim.deleteFirst") : undefined}
          onClick={() => {
            if (
              confirm(
                t("esim.confirmDelete", { name: profile.name || profile.iccid }),
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
