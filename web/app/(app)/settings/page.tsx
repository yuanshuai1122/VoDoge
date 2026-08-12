"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { PageHeader } from "@/components/layout/page-header";
import { NotificationSettingsPanel } from "@/components/settings/notification-settings";
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

const schema = z
  .object({
    old_password: z.string().min(1, "请输入当前密码"),
    new_password: z.string().min(6, "新密码至少 6 位"),
    confirm_password: z.string().min(1, "请再次输入新密码"),
  })
  .refine((v) => v.new_password === v.confirm_password, {
    message: "两次输入的新密码不一致",
    path: ["confirm_password"],
  });

type FormValues = z.infer<typeof schema>;

export default function SettingsPage() {
  const [submitError, setSubmitError] = useState<string | null>(null);

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
      toast.success("密码已修改，请重新登录");
    } catch (e) {
      setSubmitError(e instanceof ApiError ? e.message : "修改失败，请重试");
    }
  }

  return (
    <>
      <PageHeader title="系统设置" description="账号与通知配置" />

      <div className="flex max-w-2xl flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">修改密码</CardTitle>
            <CardDescription>
              修改后当前登录会立即失效，需要重新登录。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form
              onSubmit={handleSubmit(onSubmit)}
              className="flex flex-col gap-4"
            >
              <Field
                id="old_password"
                label="当前密码"
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
                label="新密码"
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
                label="确认新密码"
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
                {isSubmitting ? "提交中…" : "修改密码"}
              </Button>
            </form>
          </CardContent>
        </Card>

        <NotificationSettingsPanel />
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
