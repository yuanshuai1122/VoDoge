"use client";

import { useEffect, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ThemeProvider } from "next-themes";
import { Toaster } from "@/components/ui/sonner";
import { ApiError } from "@/lib/api/errors";
import { useLocale } from "@/lib/i18n";

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 10_000,
        refetchOnWindowFocus: false,
        retry: (failureCount, error) => {
          if (error instanceof ApiError) {
            // 401 已由 client 触发登出；权限/不存在类错误重试无意义
            if ([401, 403, 404, 400].includes(error.httpStatus)) return false;
            // eSIM 并发冲突交由 esim-busy 机制按 retryAfterMs 调度，
            // 这里重试只会加剧争抢
            if (error.busy) return false;
          }
          return failureCount < 2;
        },
      },
      mutations: {
        retry: false,
      },
    },
  });
}

function DocumentLang() {
  const locale = useLocale();
  useEffect(() => {
    document.documentElement.lang = locale === "zh" ? "zh-CN" : "en";
  }, [locale]);
  return null;
}

export function Providers({ children }: { children: React.ReactNode }) {
  // 每个浏览器会话一个实例；放 state 里避免 HMR/重渲染时重建缓存
  const [queryClient] = useState(makeQueryClient);

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider
        attribute="class"
        defaultTheme="system"
        enableSystem
        disableTransitionOnChange
      >
        <DocumentLang />
        {children}
        <Toaster position="top-right" richColors />
      </ThemeProvider>
    </QueryClientProvider>
  );
}
