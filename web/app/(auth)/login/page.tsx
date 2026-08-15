"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
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
import { login } from "@/lib/api/endpoints/auth";
import { ApiError } from "@/lib/api/errors";
import { useToken, useHydrated } from "@/hooks/use-token";
import { useT } from "@/lib/i18n";

type FormValues = { username: string; password: string };

export default function LoginPage() {
  const router = useRouter();
  const hydrated = useHydrated();
  const token = useToken();
  const t = useT();
  const [submitError, setSubmitError] = useState<string | null>(null);
  const schema = z.object({
    username: z.string().min(1, t("login.needUsername")),
    password: z.string().min(1, t("login.needPassword")),
  });

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { username: "", password: "" },
  });

  // 已登录直接进主界面。
  // 同 AuthGuard：必须等 hydration 完成，否则会与守卫来回重定向。
  useEffect(() => {
    if (hydrated && token) router.replace("/");
  }, [hydrated, token, router]);

  async function onSubmit(values: FormValues) {
    setSubmitError(null);
    try {
      await login(values.username, values.password);
      router.replace("/");
    } catch (e) {
      if (e instanceof ApiError) {
        // 后端对登录有 2 分钟 10 次/IP 的限流，需与凭证错误区分提示
        setSubmitError(
          e.isRateLimited ? t("login.rateLimited") : e.message,
        );
      } else {
        setSubmitError(t("login.failed"));
      }
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">{t("app.name")}</CardTitle>
          <CardDescription>{t("login.title")}</CardDescription>
        </CardHeader>

        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="username">{t("login.username")}</Label>
              <Input
                id="username"
                autoComplete="username"
                autoFocus
                {...register("username")}
              />
              {errors.username && (
                <p className="text-xs text-destructive">
                  {errors.username.message}
                </p>
              )}
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="password">{t("login.password")}</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                {...register("password")}
              />
              {errors.password && (
                <p className="text-xs text-destructive">
                  {errors.password.message}
                </p>
              )}
            </div>

            {submitError && (
              <Alert variant="destructive">
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            )}

            <Button type="submit" disabled={isSubmitting} className="w-full">
              {isSubmitting ? t("login.submitting") : t("login.submit")}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
