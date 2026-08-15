"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { PageHeader } from "@/components/layout/page-header";
import { NotificationSettingsPanel } from "@/components/settings/notification-settings";
import { SMSRateLimitCard } from "@/components/settings/sms-rate-limit";
import { DeviceQuotaCard } from "@/components/settings/device-quota";
import { HTTPSCard } from "@/components/settings/https-card";
import { NetworkAccessCard } from "@/components/settings/network-access";
import { SystemPanel } from "@/components/settings/system-panel";
import { DangerZone } from "@/components/settings/danger-zone";
import { PluginsCard } from "@/components/settings/plugins-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Separator } from "@/components/ui/separator";
import { changePassword } from "@/lib/api/endpoints/auth";
import { ApiError } from "@/lib/api/errors";
import { useT } from "@/lib/i18n";

type FormValues = {
  old_password: string;
  new_password: string;
  confirm_password: string;
};

export default function SettingsPage() {
  const t = useT();
  const [submitError, setSubmitError] = useState<string | null>(null);
  const schema = z
    .object({
      old_password: z.string().min(1, t("settings.needOld")),
      new_password: z.string().min(6, t("settings.needNew")),
      confirm_password: z.string().min(1, t("settings.needConfirm")),
    })
    .refine((v) => v.new_password === v.confirm_password, {
      message: t("settings.mismatch"),
      path: ["confirm_password"],
    });

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { old_password: "", new_password: "", confirm_password: "" },
  });

  async function onSubmit(values: FormValues) {
    setSubmitError(null);
    try {
      await changePassword(values);
      // changePassword 内部已触发登出：后端 token 的 HMAC 密钥就是密码，
      // 改密后所有既有 token 立即失效，AuthGuard 会把页面切回登录
      toast.success(t("settings.passwordOk"));
    } catch (e) {
      setSubmitError(e instanceof ApiError ? e.message : t("settings.passwordFailed"));
    }
  }

  return (
    <>
      <PageHeader title={t("settings.title")} description={t("settings.desc")} />

      <div className="flex max-w-2xl flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("settings.password")}</CardTitle>
            <CardDescription>{t("settings.passwordHint")}</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              onSubmit={handleSubmit(onSubmit)}
              className="flex flex-col gap-4"
            >
              <Field
                id="old_password"
                label={t("settings.oldPassword")}
                error={errors.old_password?.message}
              >
                <Input
                  id="old_password"
                  type="password"
                  autoComplete="current-password"
                  {...register("old_password")}
                />
              </Field>

              <Field
                id="new_password"
                label={t("settings.newPassword")}
                error={errors.new_password?.message}
              >
                <Input
                  id="new_password"
                  type="password"
                  autoComplete="new-password"
                  {...register("new_password")}
                />
              </Field>

              <Field
                id="confirm_password"
                label={t("settings.confirmPassword")}
                error={errors.confirm_password?.message}
              >
                <Input
                  id="confirm_password"
                  type="password"
                  autoComplete="new-password"
                  {...register("confirm_password")}
                />
              </Field>

              {submitError && (
                <Alert variant="destructive">
                  <AlertDescription>{submitError}</AlertDescription>
                </Alert>
              )}

              <Separator />

              <Button type="submit" disabled={isSubmitting} className="self-start">
                {isSubmitting ? t("common.loading") : t("settings.password")}
              </Button>
            </form>
          </CardContent>
        </Card>

        <DeviceQuotaCard />
        <HTTPSCard />
        <NetworkAccessCard />
        <SMSRateLimitCard />
        <PluginsCard />
        <SystemPanel />
        <NotificationSettingsPanel />

        {/* 放在最后：不可撤销的操作不该出现在用户随手会滑到的位置 */}
        <DangerZone />
      </div>
    </>
  );
}

function Field({
  id,
  label,
  error,
  children,
}: {
  id: string;
  label: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  );
}
