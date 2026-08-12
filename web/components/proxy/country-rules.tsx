"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
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
  listCountryRules,
  listUpstreamCountries,
  listUpstreamProxies,
  upsertCountryRule,
  deleteCountryRule,
} from "@/lib/api/endpoints/proxy";
import { ApiError } from "@/lib/api/errors";

/**
 * 按国家路由到指定上游代理。
 *
 * 国家由 SIM 的 MCC 判定，因此规则表依赖后端的 MCC/MNC 对照表；
 * 该表未就绪时后端返回 503 mcc_mnc_table_unavailable，这里需要给出可理解的提示。
 */
export function CountryRules() {
  const queryClient = useQueryClient();

  const rules = useQuery({
    queryKey: ["proxy", "country-rules"],
    queryFn: listCountryRules,
  });
  const countries = useQuery({
    queryKey: ["proxy", "countries"],
    queryFn: listUpstreamCountries,
  });
  const proxies = useQuery({
    queryKey: ["proxy", "upstream"],
    queryFn: listUpstreamProxies,
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["proxy", "country-rules"] });
  const onError = (e: unknown) =>
    toast.error(e instanceof ApiError ? e.message : "操作失败");

  const save = useMutation({
    mutationFn: (v: {
      code: string;
      upstream_proxy_id: string;
      enabled: boolean;
    }) =>
      upsertCountryRule(v.code, {
        upstream_proxy_id: v.upstream_proxy_id,
        enabled: v.enabled,
      }),
    onSuccess: () => {
      toast.success("规则已保存");
      invalidate();
    },
    onError,
  });

  const remove = useMutation({
    mutationFn: deleteCountryRule,
    onSuccess: () => {
      toast.success("规则已删除");
      invalidate();
    },
    onError,
  });

  const tableUnavailable =
    countries.isError &&
    countries.error instanceof ApiError &&
    countries.error.httpStatus === 503;

  if (tableUnavailable) {
    return (
      <Alert>
        <AlertDescription>
          MCC/MNC 国家对照表尚未就绪，无法配置按国家路由。该表由后端在启动后加载，
          请稍后重试。
        </AlertDescription>
      </Alert>
    );
  }

  if (rules.isError) {
    return <ErrorState error={rules.error} onRetry={() => rules.refetch()} />;
  }
  if (rules.isPending || countries.isPending || proxies.isPending) {
    return <Skeleton className="h-48" />;
  }

  const proxyOptions = proxies.data ?? [];
  const configured = new Set(rules.data.map((r) => r.country_code));
  const available = (countries.data ?? []).filter(
    (c) => !configured.has(c.country_code),
  );

  return (
    <div className="flex flex-col gap-3">
      {proxyOptions.length === 0 && (
        <Alert>
          <AlertDescription>
            尚未配置上游代理，需要先新增一个才能设置国家路由规则。
          </AlertDescription>
        </Alert>
      )}

      {rules.data.length === 0 ? (
        <EmptyState
          title="暂无国家规则"
          description="未匹配到规则的流量走默认出口。"
        />
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>国家</TableHead>
                <TableHead>MCC</TableHead>
                <TableHead>上游代理</TableHead>
                <TableHead>启用</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>

            <TableBody>
              {rules.data.map((r) => (
                <TableRow key={r.country_code}>
                  <TableCell>
                    <p className="font-medium">{r.country_name}</p>
                    <p className="text-xs text-muted-foreground">
                      {r.country_code}
                    </p>
                  </TableCell>

                  <TableCell className="font-mono text-xs">
                    {r.mccs?.join(", ") || "-"}
                  </TableCell>

                  <TableCell>
                    <Select
                      value={r.upstream_proxy_id}
                      onValueChange={(v) =>
                        v &&
                        save.mutate({
                          code: r.country_code,
                          upstream_proxy_id: v,
                          enabled: r.enabled,
                        })
                      }
                    >
                      <SelectTrigger className="w-44">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {proxyOptions.map((p) => (
                          <SelectItem key={p.id} value={p.id}>
                            {p.name || p.addr}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TableCell>

                  <TableCell>
                    <Switch
                      checked={r.enabled}
                      onCheckedChange={(v) =>
                        save.mutate({
                          code: r.country_code,
                          upstream_proxy_id: r.upstream_proxy_id,
                          enabled: v,
                        })
                      }
                    />
                  </TableCell>

                  <TableCell>
                    <div className="flex justify-end">
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="删除规则"
                        disabled={remove.isPending}
                        onClick={() => {
                          if (confirm(`确定删除 ${r.country_name} 的路由规则？`)) {
                            remove.mutate(r.country_code);
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

      {available.length > 0 && proxyOptions.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border p-3">
          <span className="text-sm text-muted-foreground">添加国家规则</span>
          <Select
            value=""
            onValueChange={(code) =>
              code &&
              save.mutate({
                code,
                upstream_proxy_id: proxyOptions[0].id,
                enabled: true,
              })
            }
          >
            <SelectTrigger className="w-56">
              <SelectValue placeholder="选择国家" />
            </SelectTrigger>
            <SelectContent>
              {available.map((c) => (
                <SelectItem key={c.country_code} value={c.country_code}>
                  {c.country_name}（{c.country_code}）
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-xs text-muted-foreground">
            默认绑定到「{proxyOptions[0].name || proxyOptions[0].addr}」，可在表中修改
          </span>
        </div>
      )}
    </div>
  );
}
