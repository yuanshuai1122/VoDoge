"use client";

import { useRef } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ErrorState } from "@/components/common/empty-state";
import {
  installPluginURL,
  listPlugins,
  pluginPageURL,
  uninstallPlugin,
  updatePlugin,
  uploadPlugin,
} from "@/lib/api/endpoints/extensions";
import { ApiError } from "@/lib/api/errors";
import type { InstalledPlugin } from "@/types/plugin";

const TRUST =
  "插件页面以当前管理员权限运行，插件后端还可以执行本机程序。只安装你完全信任的包。";

export function PluginsCard() {
  const queryClient = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const urlRef = useRef<HTMLInputElement>(null);
  const shaRef = useRef<HTMLInputElement>(null);

  const query = useQuery({
    queryKey: ["extensions"],
    queryFn: listPlugins,
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["extensions"] });

  const install = useMutation({
    mutationFn: (input: { url: string; sha256?: string }) =>
      installPluginURL(input),
    onSuccess: () => {
      toast.success("插件已安装并启用");
      invalidate();
    },
    onError: (e) =>
      toast.error(e instanceof ApiError ? e.message : "插件安装失败"),
  });

  const upload = useMutation({
    mutationFn: uploadPlugin,
    onSuccess: () => {
      toast.success("插件已安装并启用");
      invalidate();
    },
    onError: (e) =>
      toast.error(e instanceof ApiError ? e.message : "插件安装失败"),
  });

  const toggle = useMutation({
    mutationFn: (plugin: InstalledPlugin) =>
      updatePlugin(plugin.id, !plugin.enabled),
    onSuccess: () => invalidate(),
    onError: (e) =>
      toast.error(e instanceof ApiError ? e.message : "插件状态更新失败"),
  });

  const remove = useMutation({
    mutationFn: (id: string) => uninstallPlugin(id),
    onSuccess: () => {
      toast.success("插件已卸载");
      invalidate();
    },
    onError: (e) =>
      toast.error(e instanceof ApiError ? e.message : "插件卸载失败"),
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">插件</CardTitle>
        <CardDescription>
          通过 HTTPS URL 或本地 zip 扩展控制台。{TRUST}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid gap-3 md:grid-cols-[1fr_12rem_auto]">
          <div className="flex flex-col gap-2">
            <Label htmlFor="plugin-url">插件包 URL</Label>
            <Input
              id="plugin-url"
              ref={urlRef}
              placeholder="https://example.com/plugin.zip"
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="plugin-sha">SHA-256（可选）</Label>
            <Input id="plugin-sha" ref={shaRef} placeholder="推荐填写" />
          </div>
          <Button
            type="button"
            className="self-end"
            disabled={install.isPending}
            onClick={() => {
              const url = urlRef.current?.value.trim() ?? "";
              if (!url) {
                toast.error("请输入插件包 URL");
                return;
              }
              if (!window.confirm(`安装外部插件？\n${TRUST}`)) return;
              install.mutate({
                url,
                sha256: shaRef.current?.value.trim() || undefined,
              });
            }}
          >
            {install.isPending ? "安装中…" : "从 URL 安装"}
          </Button>
        </div>

        <div>
          <input
            ref={fileRef}
            type="file"
            accept=".zip,.vodoge-plugin,application/zip"
            className="hidden"
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (!file) return;
              if (!window.confirm(`安装上传的插件？\n${TRUST}`)) {
                event.target.value = "";
                return;
              }
              upload.mutate(file);
              event.target.value = "";
            }}
          />
          <Button
            type="button"
            variant="outline"
            disabled={upload.isPending}
            onClick={() => fileRef.current?.click()}
          >
            {upload.isPending ? "上传中…" : "上传插件包"}
          </Button>
        </div>

        {query.isPending ? (
          <Skeleton className="h-24" />
        ) : query.data && query.data.length > 0 ? (
          <ul className="flex flex-col gap-3">
            {query.data.map((plugin) => (
              <li
                key={plugin.id}
                className="flex flex-col gap-3 rounded-md border p-3 sm:flex-row sm:items-center"
              >
                <div className="min-w-0 flex-1">
                  <p className="font-medium">
                    {plugin.name}{" "}
                    <span className="text-xs font-normal text-muted-foreground">
                      v{plugin.version}
                    </span>
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {plugin.description || plugin.id}
                  </p>
                  {plugin.backend_error ? (
                    <p className="mt-1 text-xs text-destructive">
                      {plugin.backend_error}
                    </p>
                  ) : null}
                  {plugin.enabled
                    ? plugin.contributions
                        .filter((c) => c.location === "sidebar")
                        .map((c) => (
                          <Link
                            key={c.id}
                            href={pluginPageURL(plugin.id, c.id)}
                            className="mt-2 inline-block text-xs underline"
                          >
                            打开 {c.label_zh || c.label}
                          </Link>
                        ))
                    : null}
                </div>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={toggle.isPending}
                    onClick={() => toggle.mutate(plugin)}
                  >
                    {plugin.enabled ? "禁用" : "启用"}
                  </Button>
                  <Button
                    type="button"
                    variant="destructive"
                    disabled={remove.isPending}
                    onClick={() => {
                      if (!window.confirm("卸载插件？代码和插件数据将从本机删除。"))
                        return;
                      remove.mutate(plugin.id);
                    }}
                  >
                    卸载
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">尚未安装插件</p>
        )}
      </CardContent>
    </Card>
  );
}
