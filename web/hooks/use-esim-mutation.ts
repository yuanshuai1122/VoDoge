"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/errors";
import { useEsimLockStore } from "@/stores/esim-lock";

/**
 * 统一封装 eSIM 写操作：
 *  - 执行期间标记设备为 running，禁用同设备其它入口
 *  - 遇到 409 ESIM_BUSY 时记录后端给的冷却窗口，而不是立即重试
 *  - 成功后刷新该设备的 eSIM 查询
 */
export function useEsimMutation<TArgs, TResult>(options: {
  deviceId: string;
  operation: string;
  mutationFn: (args: TArgs) => Promise<TResult>;
  successMessage?: string | ((result: TResult) => string);
  onSuccess?: (result: TResult) => void;
}) {
  const { deviceId, operation, mutationFn, successMessage, onSuccess } = options;
  const queryClient = useQueryClient();
  const begin = useEsimLockStore((s) => s.begin);
  const end = useEsimLockStore((s) => s.end);
  const markBusy = useEsimLockStore((s) => s.markBusy);

  return useMutation({
    mutationFn: async (args: TArgs) => {
      begin(deviceId, operation);
      try {
        return await mutationFn(args);
      } finally {
        end(deviceId);
      }
    },
    onSuccess: (result) => {
      if (successMessage) {
        toast.success(
          typeof successMessage === "function"
            ? successMessage(result)
            : successMessage,
        );
      }
      queryClient.invalidateQueries({ queryKey: ["esim", deviceId] });
      onSuccess?.(result);
    },
    onError: (e) => {
      if (e instanceof ApiError && e.busy) {
        // 占用方可能是其它客户端或后台任务，按后端给的窗口等待
        markBusy(deviceId, e.retryAfterMs, e.reason);
        const seconds = Math.ceil((e.retryAfterMs ?? 3000) / 1000);
        toast.warning(`eSIM 正忙，请等待约 ${seconds} 秒后重试`);
        return;
      }
      toast.error(e instanceof ApiError ? e.message : "操作失败");
    },
  });
}
