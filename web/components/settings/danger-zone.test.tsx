import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DangerZone } from "./danger-zone";

/**
 * 卸载会删掉数据目录、配置文件和程序自身，没有任何补救办法。
 * 这里守的是"不可能误触"这条约束——它是这个入口能存在的前提。
 */

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/system", () => ({ uninstall: vi.fn() }));

const { uninstall } = await import("@/lib/api/endpoints/system");

function renderZone() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <DangerZone />
    </QueryClientProvider>,
  );
}

const submitButton = () =>
  screen.getByRole("button", { name: /卸载并删除本机数据/ });

describe("DangerZone", () => {
  beforeEach(() => {
    localStorage.setItem("vodoge.token", "tok");
  });
  afterEach(() => vi.clearAllMocks());

  it("默认不可提交", () => {
    renderZone();
    expect(submitButton()).toBeDisabled();
  });

  // 输错、大小写不符、多打少打都不算数——差一个字符就不该解锁。
  it("确认词不完全一致时保持禁用", async () => {
    renderZone();
    const input = screen.getByLabelText(/确认请输入/);

    for (const wrong of ["uninstall", "UNINSTAL", "UNINSTALL "]) {
      await userEvent.clear(input);
      await userEvent.type(input, wrong);
      expect(submitButton()).toBeDisabled();
    }
    expect(uninstall).not.toHaveBeenCalled();
  });

  it("逐字输入确认词后才解锁", async () => {
    renderZone();
    await userEvent.type(screen.getByLabelText(/确认请输入/), "UNINSTALL");
    expect(submitButton()).toBeEnabled();
  });

  // 事前必须说清代价：删什么、以及 PostgreSQL 不在范围内。
  it("列出会被删除的东西，并声明数据库不在其中", () => {
    renderZone();
    expect(screen.getByText(/删除程序自身/)).toBeInTheDocument();
    expect(screen.getByText(/禁用开机自启/)).toBeInTheDocument();
    expect(screen.getByText(/PostgreSQL/)).toBeInTheDocument();
  });

  // 后端回 200 一秒后就把自己删了。成功即终态，不该再等任何东西。
  it("成功后进入终态并清掉本地凭证", async () => {
    vi.mocked(uninstall).mockResolvedValue(undefined);

    renderZone();
    await userEvent.type(screen.getByLabelText(/确认请输入/), "UNINSTALL");
    await userEvent.click(submitButton());

    await waitFor(() =>
      expect(screen.getByText(/卸载指令已下发/)).toBeInTheDocument(),
    );
    expect(screen.getByText(/本页面不再可用/)).toBeInTheDocument();
    expect(localStorage.getItem("vodoge.token")).toBeNull();
  });

  // 失败后确认词要清空：不能让上一次的输入继续处于"待发射"状态。
  it("失败后清空确认词并重新锁上", async () => {
    vi.mocked(uninstall).mockRejectedValue(new Error("boom"));

    renderZone();
    await userEvent.type(screen.getByLabelText(/确认请输入/), "UNINSTALL");
    await userEvent.click(submitButton());

    await waitFor(() => expect(submitButton()).toBeDisabled());
    expect(screen.getByLabelText(/确认请输入/)).toHaveValue("");
    // 凭证不该被清掉——服务还活着
    expect(localStorage.getItem("vodoge.token")).toBe("tok");
  });
});
