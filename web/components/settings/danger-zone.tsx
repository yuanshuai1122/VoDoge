"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { uninstall } from "@/lib/api/endpoints/system";
import { ApiError } from "@/lib/api/errors";
import { clearToken } from "@/lib/auth/token";

/** 必须逐字输入才允许提交。 */
const CONFIRM_PHRASE = "UNINSTALL";

/**
 * 危险操作区：卸载 / 自毁。
 *
 * 这个入口此前是刻意不做的——后端会删掉自己，做错一次没有任何补救办法。
 * 既然要做，就得让它难到不可能误触：
 *
 *  - **输入确认词**，不是点两下。点击可以手滑，逐字输入 `UNINSTALL` 不会。
 *  - **明确列出会被删掉什么**。用户需要在按下之前就知道代价，
 *    而不是事后从日志里发现。
 *  - **成功即终态**。后端回 200 之后一秒就把自己删了并退出，
 *    此时转圈等结果、或者刷新页面重试，都是在等一个永远不会来的响应。
 *    所以这里直接切到"已发出、服务即将消失"的说明，并清掉本地凭证。
 */
export function DangerZone() {
  const [phrase, setPhrase] = useState("");
  const [fired, setFired] = useState(false);

  const run = useMutation({
    mutationFn: uninstall,
    onSuccess: () => {
      // 服务马上就没了：清掉本地 token，避免下次打开页面时以"已登录"
      // 的样子卡在所有请求都失败的界面上。
      clearToken();
      setFired(true);
    },
    onError: (e) => {
      // 清空确认词：失败后要重新逐字输入，不能让上一次的输入继续处于"待发射"状态
      setPhrase("");
      toast.error(e instanceof ApiError ? e.message : "卸载指令下发失败");
    },
  });

  if (fired) {
    return (
      <Card className="border-destructive/50">
        <CardHeader>
          <CardTitle className="text-base text-destructive">
            卸载指令已下发
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Alert variant="destructive">
            <AlertDescription>
              服务会在数秒内停止自启、删除数据目录与配置文件、删除自身可执行文件，
              然后退出。<strong>本页面不再可用</strong>，后续请求都会失败——这是预期结果。
              <span className="mt-2 block">
                PostgreSQL 里的数据不在删除范围内，需要你自行处理。
              </span>
            </AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    );
  }

  const armed = phrase === CONFIRM_PHRASE;

  return (
    <Card className="border-destructive/50">
      <CardHeader className="flex-row items-center gap-2 space-y-0">
        <AlertTriangle className="size-4 text-destructive" />
        <CardTitle className="text-base text-destructive">危险操作</CardTitle>
      </CardHeader>

      <CardContent className="flex flex-col gap-4">
        <Alert variant="destructive">
          <AlertDescription>
            <p className="font-medium">卸载会做以下事，且无法撤销：</p>
            <ul className="mt-2 list-disc space-y-1 pl-5">
              <li>停止服务并<strong>禁用开机自启</strong></li>
              <li>
                删除数据目录 <code className="font-mono text-xs">data/</code>
              </li>
              <li>删除当前加载的配置文件（含登录凭据与设备配置）</li>
              <li>
                <strong>删除程序自身</strong>，随后进程退出
              </li>
            </ul>
            <p className="mt-2">
              PostgreSQL 里的业务数据<strong>不会</strong>被删除——它是外部服务。
              需要一并清理的话请用 <code className="font-mono text-xs">psql</code> 自行处理。
            </p>
          </AlertDescription>
        </Alert>

        <div className="flex flex-col gap-2">
          <Label htmlFor="uninstall_confirm">
            确认请输入 <code className="font-mono">{CONFIRM_PHRASE}</code>
          </Label>
          <Input
            id="uninstall_confirm"
            className="max-w-xs font-mono"
            autoComplete="off"
            placeholder={CONFIRM_PHRASE}
            value={phrase}
            disabled={run.isPending}
            onChange={(e) => setPhrase(e.target.value)}
          />
        </div>

        <Button
          variant="destructive"
          size="sm"
          className="self-start"
          disabled={!armed || run.isPending}
          onClick={() => run.mutate()}
        >
          {run.isPending ? "下发中…" : "卸载并删除本机数据"}
        </Button>
      </CardContent>
    </Card>
  );
}
