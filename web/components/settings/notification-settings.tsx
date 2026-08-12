"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Send } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState } from "@/components/common/empty-state";
import {
  getNotificationSettings,
  updateNotificationSettings,
  testNotification,
  type TestableChannel,
} from "@/lib/api/endpoints/system";
import { ApiError } from "@/lib/api/errors";
import type { NotificationSettings } from "@/types/notification";

export function NotificationSettingsPanel() {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ["settings", "notifications"],
    queryFn: () => getNotificationSettings() as Promise<NotificationSettings>,
  });

  const save = useMutation({
    mutationFn: (input: NotificationSettings) =>
      updateNotificationSettings(input),
    onSuccess: () => {
      toast.success("通知设置已保存");
      queryClient.invalidateQueries({ queryKey: ["settings", "notifications"] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => query.refetch()} />;
  }
  if (query.isPending) return <Skeleton className="h-96" />;

  return (
    <NotificationForm
      initial={query.data}
      saving={save.isPending}
      onSave={(v) => save.mutate(v)}
    />
  );
}

function NotificationForm({
  initial,
  saving,
  onSave,
}: {
  initial: NotificationSettings;
  saving: boolean;
  onSave: (v: NotificationSettings) => void;
}) {
  const [s, setS] = useState<NotificationSettings>(initial);

  // 逐渠道更新，保持其余渠道原样：后端是整体替换
  function patch<K extends keyof NotificationSettings>(
    channel: K,
    values: Partial<NotificationSettings[K]>,
  ) {
    setS((prev) => ({ ...prev, [channel]: { ...prev[channel], ...values } }));
  }

  return (
    <div className="flex flex-col gap-4">
      <Channel
        title="Telegram"
        enabled={s.telegram?.enabled ?? false}
        onToggle={(v) => patch("telegram", { enabled: v })}
      >
        <Text
          label="Bot Token"
          value={s.telegram?.bot_token ?? ""}
          onChange={(v) => patch("telegram", { bot_token: v })}
        />
        <Num
          label="Chat ID"
          value={s.telegram?.chat_id ?? 0}
          onChange={(v) => patch("telegram", { chat_id: v })}
        />
        <Num
          label="Admin ID"
          value={s.telegram?.admin_id ?? 0}
          onChange={(v) => patch("telegram", { admin_id: v })}
        />
        <Text
          label="API 代理地址"
          value={s.telegram?.proxy ?? ""}
          hint="中国大陆服务器通常需要配置代理才能访问 Telegram API"
          onChange={(v) => patch("telegram", { proxy: v })}
        />
      </Channel>

      <Channel
        title="Webhook"
        enabled={s.webhook?.enabled ?? false}
        testable="webhook"
        onToggle={(v) => patch("webhook", { enabled: v })}
      >
        <List
          label="URL 列表"
          value={s.webhook?.urls ?? []}
          onChange={(v) => patch("webhook", { urls: v })}
        />
        <Text
          label="签名密钥"
          value={s.webhook?.secret ?? ""}
          onChange={(v) => patch("webhook", { secret: v })}
        />
        <Num
          label="超时 (ms)"
          value={s.webhook?.timeout_ms ?? 0}
          onChange={(v) => patch("webhook", { timeout_ms: v })}
        />
        <Num
          label="最大重试"
          value={s.webhook?.retry_max ?? 0}
          onChange={(v) => patch("webhook", { retry_max: v })}
        />
      </Channel>

      <Channel
        title="Bark"
        enabled={s.bark?.enabled ?? false}
        testable="bark"
        onToggle={(v) => patch("bark", { enabled: v })}
      >
        <List
          label="推送地址"
          value={s.bark?.urls ?? []}
          onChange={(v) => patch("bark", { urls: v })}
        />
        <Text
          label="分组"
          value={s.bark?.group ?? ""}
          onChange={(v) => patch("bark", { group: v })}
        />
        <Text
          label="级别"
          value={s.bark?.level ?? ""}
          onChange={(v) => patch("bark", { level: v })}
        />
      </Channel>

      <Channel
        title="邮件"
        enabled={s.email?.enabled ?? false}
        testable="email"
        onToggle={(v) => patch("email", { enabled: v })}
      >
        <Text
          label="SMTP 主机"
          value={s.email?.smtp_host ?? ""}
          onChange={(v) => patch("email", { smtp_host: v })}
        />
        <Num
          label="端口"
          value={s.email?.smtp_port ?? 0}
          onChange={(v) => patch("email", { smtp_port: v })}
        />
        <Text
          label="用户名"
          value={s.email?.username ?? ""}
          onChange={(v) => patch("email", { username: v })}
        />
        <Text
          label="密码"
          type="password"
          value={s.email?.password ?? ""}
          onChange={(v) => patch("email", { password: v })}
        />
        <Text
          label="发件地址"
          value={s.email?.from_address ?? ""}
          onChange={(v) => patch("email", { from_address: v })}
        />
        <List
          label="收件地址"
          value={s.email?.to_addresses ?? []}
          onChange={(v) => patch("email", { to_addresses: v })}
        />
        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="email_ssl">使用 SSL</Label>
          <Switch
            id="email_ssl"
            checked={s.email?.use_ssl ?? false}
            onCheckedChange={(v) => patch("email", { use_ssl: v })}
          />
        </div>
      </Channel>

      <Channel
        title="PushPlus"
        enabled={s.pushplus?.enabled ?? false}
        onToggle={(v) => patch("pushplus", { enabled: v })}
      >
        <Text
          label="Token"
          value={s.pushplus?.token ?? ""}
          onChange={(v) => patch("pushplus", { token: v })}
        />
        <Text
          label="Topic"
          value={s.pushplus?.topic ?? ""}
          onChange={(v) => patch("pushplus", { topic: v })}
        />
      </Channel>

      <Channel
        title="飞书"
        enabled={s.feishu?.enabled ?? false}
        onToggle={(v) => patch("feishu", { enabled: v })}
      >
        <Text
          label="App ID"
          value={s.feishu?.app_id ?? ""}
          onChange={(v) => patch("feishu", { app_id: v })}
        />
        <Text
          label="App Secret"
          type="password"
          value={s.feishu?.app_secret ?? ""}
          onChange={(v) => patch("feishu", { app_secret: v })}
        />
        <List
          label="会话 ID"
          value={s.feishu?.chat_ids ?? []}
          onChange={(v) => patch("feishu", { chat_ids: v })}
        />
      </Channel>

      <Channel
        title="QQ"
        enabled={s.qq?.enabled ?? false}
        onToggle={(v) => patch("qq", { enabled: v })}
      >
        <Text
          label="App ID"
          value={s.qq?.app_id ?? ""}
          onChange={(v) => patch("qq", { app_id: v })}
        />
        <Text
          label="App Secret"
          type="password"
          value={s.qq?.app_secret ?? ""}
          onChange={(v) => patch("qq", { app_secret: v })}
        />
        <Text
          label="群 ID"
          value={s.qq?.group_ids ?? ""}
          onChange={(v) => patch("qq", { group_ids: v })}
        />
      </Channel>

      <Button
        className="self-start"
        disabled={saving}
        onClick={() => onSave(s)}
      >
        {saving ? "保存中…" : "保存通知设置"}
      </Button>
    </div>
  );
}

function Channel({
  title,
  enabled,
  testable,
  onToggle,
  children,
}: {
  title: string;
  enabled: boolean;
  /** 仅 webhook / bark / email 有测试接口，其余渠道不提供按钮以免误导 */
  testable?: TestableChannel;
  onToggle: (v: boolean) => void;
  children: React.ReactNode;
}) {
  const test = useMutation({
    mutationFn: () => testNotification(testable!),
    onSuccess: () => toast.success("测试消息已发送"),
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "测试失败"),
  });

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-4">
        <CardTitle className="text-base">{title}</CardTitle>
        <div className="flex items-center gap-2">
          {testable && (
            <Button
              variant="outline"
              size="sm"
              disabled={!enabled || test.isPending}
              onClick={() => test.mutate()}
            >
              <Send className="size-3.5" />
              {test.isPending ? "发送中…" : "测试"}
            </Button>
          )}
          <Switch checked={enabled} onCheckedChange={onToggle} />
        </div>
      </CardHeader>

      {enabled && (
        <CardContent className="flex flex-col gap-3">{children}</CardContent>
      )}
    </Card>
  );
}

function Text({
  label,
  value,
  onChange,
  type,
  hint,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  type?: string;
  hint?: string;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <Input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

function Num({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <Input
        type="number"
        value={String(value)}
        onChange={(e) => onChange(Number(e.target.value) || 0)}
      />
    </div>
  );
}

/** 数组字段用换行分隔编辑，比动态增删行更省心。 */
function List({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string[];
  onChange: (v: string[]) => void;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <Input
        value={value.join(", ")}
        placeholder="多个值用逗号分隔"
        onChange={(e) =>
          onChange(
            e.target.value
              .split(",")
              .map((v) => v.trim())
              .filter(Boolean),
          )
        }
      />
    </div>
  );
}
