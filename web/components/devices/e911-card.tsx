"use client";

import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CheckCircle2, ExternalLink } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { API_BASE } from "@/lib/api/client";
import { ApiError } from "@/lib/api/errors";
import {
  getWebsheetStatus,
  startE911Websheet,
  type WebsheetInfo,
} from "@/lib/api/endpoints/e911";

/**
 * E911 紧急地址登记。
 *
 * 表单是运营商自己的页面，后端代理它并注入桥接脚本。这里**新开窗口**而不是内嵌
 * iframe，理由有三：
 *  - 页面内容不受我们控制，其 CSP / X-Frame-Options 随时可能拒绝被内嵌；
 *  - 运营商流程常跳转到第三方（身份校验、地址库），iframe 内更容易断链；
 *  - 完成与否本来就得靠服务端的桥接回调判断，内嵌并不能换来更多信号。
 *
 * 所以完成状态靠轮询 /websheets/:id/status，而不是监听窗口事件。
 */
export function E911Card({
  deviceId,
  available,
}: {
  deviceId: string;
  /** 来自设备概览的 e911_setup_available；false 时只做说明，不给入口 */
  available?: boolean;
}) {
  const queryClient = useQueryClient();
  const [session, setSession] = useState<WebsheetInfo | null>(null);
  // 弹窗被拦截时留一个二次手势的入口——第二次 open 由用户点击直接触发，不会被拦
  const [blocked, setBlocked] = useState(false);
  const windowRef = useRef<Window | null>(null);
  const notifiedRef = useRef<string | null>(null);

  const status = useQuery({
    queryKey: ["websheet", session?.id],
    // 终态的收尾放在取数函数里而不是 effect 里：这里本来就是与外部状态同步的
    // 边界，effect 里改 state 只会多一轮渲染
    queryFn: async () => {
      const st = await getWebsheetStatus(session!.id);
      if (st.finished && notifiedRef.current !== st.id) {
        notifiedRef.current = st.id;
        windowRef.current?.close();
        windowRef.current = null;
        toast.success("紧急地址登记流程已完成");
        queryClient.invalidateQueries({
          queryKey: ["devices", "overview", deviceId],
        });
      }
      return st;
    },
    enabled: session !== null,
    // 会话 TTL 10 分钟，3s 一次足够及时又不至于压垮后端
    refetchInterval: (q) => (q.state.data?.finished ? false : 3_000),
    retry: false,
  });

  const finished = status.data?.finished === true;
  // 会话过期（410）或被清理（404）：不再轮询，也不当作成功
  const gone =
    status.error instanceof ApiError &&
    (status.error.httpStatus === 410 || status.error.httpStatus === 404);
  const waiting = session !== null && !finished && !gone;

  const start = useMutation({
    mutationFn: async () => {
      // 必须在点击的同步阶段先开窗口：等 POST 回来再开会被弹窗拦截器拦下
      const win = window.open("about:blank", "_blank");
      try {
        const info = await startE911Websheet(deviceId);
        if (win) {
          win.location.replace(`${API_BASE}${info.embedUrl}`);
          windowRef.current = win;
        }
        setBlocked(win === null);
        return info;
      } catch (err) {
        win?.close();
        throw err;
      }
    },
    onSuccess: (info) => setSession(info),
    onError: (err) =>
      toast.error(err instanceof ApiError ? err.message : "发起登记失败"),
  });

  function reset() {
    windowRef.current?.close();
    windowRef.current = null;
    notifiedRef.current = null;
    setSession(null);
    setBlocked(false);
  }

  function openAgain() {
    if (!session) return;
    const win = window.open(`${API_BASE}${session.embedUrl}`, "_blank");
    if (win) {
      windowRef.current = win;
      setBlocked(false);
    }
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-3 space-y-0">
        <CardTitle className="text-base">E911 紧急地址</CardTitle>
        {waiting && <Badge variant="secondary">登记中</Badge>}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          VoWiFi 通话拨打紧急号码时，运营商需要一个已登记的实际地址。
          登记表单由运营商提供，会在新窗口打开。
        </p>

        {available === false && (
          <Alert>
            <AlertDescription>
              当前网络的运营商未启用 E911 登记，或尚未读到归属 PLMN。
            </AlertDescription>
          </Alert>
        )}

        {blocked && waiting && (
          <Alert>
            <AlertDescription>
              浏览器拦截了新窗口。会话已创建，点下面的按钮重新打开即可。
            </AlertDescription>
          </Alert>
        )}

        {waiting && !blocked && (
          <p className="text-xs text-muted-foreground">
            请在新窗口中完成表单。完成后本页会自动更新——期间请勿关闭本页。
            {status.data?.event && (
              <span className="ml-1 font-mono">[{status.data.event}]</span>
            )}
          </p>
        )}

        {finished && (
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <CheckCircle2 className="size-3.5 text-emerald-500" />
            登记流程已完成
            {status.data?.result_code && (
              <span className="font-mono">
                （result_code {status.data.result_code}）
              </span>
            )}
          </p>
        )}

        {gone && (
          <Alert variant="destructive">
            <AlertDescription>登记会话已过期，请重新发起。</AlertDescription>
          </Alert>
        )}

        <div className="flex gap-2">
          <Button
            size="sm"
            disabled={
              start.isPending || waiting || available === false
            }
            onClick={() => {
              reset();
              start.mutate();
            }}
          >
            <ExternalLink className="size-3.5" />
            {start.isPending
              ? "正在建立会话…"
              : waiting
                ? "等待表单完成…"
                : finished || gone
                  ? "重新登记"
                  : "登记紧急地址"}
          </Button>
          {blocked && waiting && (
            <Button size="sm" variant="outline" onClick={openAgain}>
              重新打开窗口
            </Button>
          )}
          {waiting && (
            <Button size="sm" variant="ghost" onClick={reset}>
              取消
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
