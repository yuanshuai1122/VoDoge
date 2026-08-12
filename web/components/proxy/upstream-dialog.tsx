"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  createUpstreamProxy,
  updateUpstreamProxy,
  type UpstreamProxy,
} from "@/lib/api/endpoints/proxy";
import { ApiError } from "@/lib/api/errors";

/**
 * 新增 / 编辑上游代理。
 *
 * 密码有个坑：列表接口返回的是**脱敏后**的占位串（后端 maskSecret）。
 * 编辑时若把它原样提交，就会把真实密码覆盖成一串星号。
 * 因此编辑模式下密码框初始为空，留空即表示「不修改」。
 */
export function UpstreamDialog({
  open,
  onOpenChange,
  editing,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** 传入表示编辑，否则为新增 */
  editing?: UpstreamProxy | null;
}) {
  const isEdit = Boolean(editing);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? "编辑上游代理" : "新增上游代理"}</DialogTitle>
          <DialogDescription>
            上游代理用于二次转发出站流量，地址格式为 host:port。
          </DialogDescription>
        </DialogHeader>

        {/* key 保证切换编辑对象时表单重建 */}
        <UpstreamForm
          key={editing?.id ?? "new"}
          editing={editing ?? null}
          onDone={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  );
}

function UpstreamForm({
  editing,
  onDone,
}: {
  editing: UpstreamProxy | null;
  onDone: () => void;
}) {
  const queryClient = useQueryClient();

  const [name, setName] = useState(editing?.name ?? "");
  const [addr, setAddr] = useState(editing?.addr ?? "");
  const [username, setUsername] = useState(editing?.username ?? "");
  // 编辑时故意不预填：列表里的密码是脱敏占位值
  const [password, setPassword] = useState("");
  const [enabled, setEnabled] = useState(editing?.enabled ?? true);

  const save = useMutation({
    mutationFn: async () => {
      const payload: Partial<UpstreamProxy> = {
        name: name.trim(),
        addr: addr.trim(),
        username: username.trim(),
        enabled,
      };
      // 留空表示不修改密码，不能提交空串（会清掉真实密码）
      if (password) payload.password = password;

      if (editing) {
        await updateUpstreamProxy(editing.id, payload);
      } else {
        await createUpstreamProxy(payload);
      }
    },
    onSuccess: () => {
      toast.success(editing ? "已保存" : "已新增");
      queryClient.invalidateQueries({ queryKey: ["proxy", "upstream"] });
      onDone();
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "保存失败"),
  });

  return (
    <>
      <div className="flex flex-col gap-4">
        <Field id="up_name" label="名称">
          <Input
            id="up_name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>

        <Field id="up_addr" label="地址">
          <Input
            id="up_addr"
            placeholder="127.0.0.1:1080"
            value={addr}
            onChange={(e) => setAddr(e.target.value)}
          />
        </Field>

        <Field id="up_user" label="用户名（可选）">
          <Input
            id="up_user"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </Field>

        <Field
          id="up_pass"
          label="密码（可选）"
          hint={editing ? "留空表示不修改现有密码" : undefined}
        >
          <Input
            id="up_pass"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>

        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="up_enabled">启用</Label>
          <Switch
            id="up_enabled"
            checked={enabled}
            onCheckedChange={setEnabled}
          />
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onDone}>
          取消
        </Button>
        <Button
          disabled={save.isPending || !addr.trim()}
          onClick={() => save.mutate()}
        >
          {save.isPending ? "保存中…" : "保存"}
        </Button>
      </DialogFooter>
    </>
  );
}

function Field({
  id,
  label,
  hint,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
