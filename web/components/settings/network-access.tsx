"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ErrorState } from "@/components/common/empty-state";
import {
  getSecuritySettings,
  updateSecuritySettings,
} from "@/lib/api/endpoints/system";
import { ApiError } from "@/lib/api/errors";

export function NetworkAccessCard() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["settings", "security"],
    queryFn: getSecuritySettings,
  });
  const [mode, setMode] = useState("internal");
  const [cidrs, setCidrs] = useState<string[]>([]);
  const [trustProxy, setTrustProxy] = useState(false);

  useEffect(() => {
    if (!query.data) return;
    setMode(query.data.mode || "internal");
    setCidrs(query.data.allowed_cidrs?.length ? [...query.data.allowed_cidrs] : []);
    setTrustProxy(!!query.data.trust_proxy_headers);
  }, [query.data]);

  const save = useMutation({
    mutationFn: () =>
      updateSecuritySettings({
        mode,
        allowed_cidrs: cidrs.map((c) => c.trim()).filter(Boolean),
        trust_proxy_headers: trustProxy,
      }),
    onSuccess: (data) => {
      toast.success(
        data.client_allowed
          ? "访问策略已保存"
          : "已保存，但当前连接可能被拒绝",
      );
      queryClient.invalidateQueries({ queryKey: ["settings", "security"] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">网络访问</CardTitle>
        <CardDescription>
          控制谁能打开管理面。默认只放行内网，避免把 7575 直接暴露到公网。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {query.isPending ? (
          <Skeleton className="h-32" />
        ) : (
          <>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                size="sm"
                variant={mode === "internal" ? "default" : "outline"}
                onClick={() => setMode("internal")}
              >
                内网优先
              </Button>
              <Button
                type="button"
                size="sm"
                variant={mode === "public" ? "default" : "outline"}
                onClick={() => setMode("public")}
              >
                对公网开放
              </Button>
            </div>
            <p className="text-sm text-muted-foreground">
              {mode === "public"
                ? "允许任意来源 IP。只在已设强密码、且环境可信时使用。"
                : "默认放行 10/8、172.16/12、192.168/16、169.254/16、127/8、::1、链路本地和 IPv6 ULA。"}
            </p>
            {mode === "public" && (
              <Alert>
                <AlertDescription>
                  对公网开放会扩大攻击面，请用强密码，并尽快切回内网优先。
                </AlertDescription>
              </Alert>
            )}

            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-2">
                <p className="text-sm font-medium">额外放行网段</p>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setCidrs((v) => [...v, ""])}
                >
                  <Plus className="size-4" />
                  添加
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                CIDR 或单个 IP。内网优先时，这些地址也会放行。
              </p>
              {cidrs.length === 0 ? (
                <p className="text-xs text-muted-foreground">暂无额外网段</p>
              ) : (
                cidrs.map((cidr, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <Input
                      className="font-mono"
                      value={cidr}
                      placeholder="203.0.113.0/24"
                      onChange={(e) =>
                        setCidrs((v) =>
                          v.map((item, idx) => (idx === i ? e.target.value : item)),
                        )
                      }
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label="删除网段"
                      onClick={() =>
                        setCidrs((v) => v.filter((_, idx) => idx !== i))
                      }
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                ))
              )}
            </div>

            <div className="flex items-start justify-between gap-4 rounded-md border p-3">
              <div className="space-y-1">
                <p className="text-sm font-medium">信任代理请求头</p>
                <p className="text-xs text-muted-foreground">
                  仅在前面有可信反代时打开。否则客户端可伪造 X-Forwarded-For
                  绕过内网限制。
                </p>
              </div>
              <Switch
                checked={trustProxy}
                onCheckedChange={setTrustProxy}
                aria-label="信任代理请求头"
              />
            </div>

            <p className="text-xs text-muted-foreground">
              当前连接 {query.data?.client_ip || "--"}
              {query.data?.client_allowed === false
                ? " 不在放行范围，保存后可能无法继续访问。"
                : " 可以访问。"}
            </p>

            <Button
              type="button"
              className="self-start"
              disabled={save.isPending}
              onClick={() => save.mutate()}
            >
              {save.isPending ? "保存中…" : "保存访问策略"}
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  );
}
