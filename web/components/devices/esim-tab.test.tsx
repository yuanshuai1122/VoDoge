import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { EsimTab } from "./esim-tab";
import type { EUICCProfiles } from "@/types/esim";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/esim", () => ({
  listProfiles: vi.fn(),
  switchProfile: vi.fn(),
  disableProfile: vi.fn(),
  renameProfile: vi.fn(),
  deleteProfile: vi.fn(),
  listNotifications: vi.fn().mockResolvedValue([]),
  getChipInfo: vi.fn().mockResolvedValue({}),
  retryNotification: vi.fn(),
  downloadProfileStreamPath: (id: string) =>
    `/devices/${id}/esim/actions/download/stream`,
  startDownloadProfile: vi.fn(),
}));

const esim = await import("@/lib/api/endpoints/esim");

const groups: EUICCProfiles[] = [
  {
    eid: "89044045",
    aid_hex: "A0000005591010",
    profiles: [
      {
        iccid: "8986001",
        name: "备用",
        service_provider_name: "CMCC",
        state: 0,
        state_text: "已禁用",
      },
      {
        iccid: "8986002",
        name: "国外号",
        service_provider_name: "Airalo",
        state: 1,
        state_text: "已启用",
      },
    ],
  },
];

function renderTab() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <EsimTab deviceId="dev-intl" />
    </QueryClientProvider>,
  );
}

describe("EsimTab", () => {
  beforeEach(() => {
    vi.mocked(esim.listProfiles).mockReset();
    vi.mocked(esim.switchProfile).mockReset();
    vi.mocked(esim.disableProfile).mockReset();
    vi.mocked(esim.listProfiles).mockResolvedValue(groups);
    vi.mocked(esim.switchProfile).mockResolvedValue({
      message: "ok",
      target_iccid: "8986001",
      switch_accepted: true,
      recovery_pending: false,
    });
    vi.mocked(esim.disableProfile).mockResolvedValue({
      target_iccid: "8986002",
      message: "已禁用",
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
  });

  it("把当前启用的 Profile 放在顶部", async () => {
    renderTab();
    expect(await screen.findByText("当前号码")).toBeInTheDocument();
    expect(screen.getAllByText("国外号").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "禁用当前卡" })).toBeEnabled();
  });

  it("启用前要确认，取消则不发请求", async () => {
    const user = userEvent.setup();
    vi.mocked(window.confirm).mockReturnValueOnce(false);
    renderTab();
    await screen.findByText("当前号码");
    await user.click(screen.getByRole("button", { name: "启用" }));
    expect(esim.switchProfile).not.toHaveBeenCalled();
  });

  it("确认后切换，并带上 aid", async () => {
    const user = userEvent.setup();
    renderTab();
    await screen.findByText("当前号码");
    await user.click(screen.getByRole("button", { name: "启用" }));
    expect(esim.switchProfile).toHaveBeenCalledWith("dev-intl", {
      iccid: "8986001",
      aid_hex: "A0000005591010",
    });
  });

  it("使用中的 Profile 不能删", async () => {
    renderTab();
    await screen.findByText("当前号码");
    const deletes = screen.getAllByLabelText("删除");
    expect(deletes[1]).toBeDisabled();
    expect(deletes[0]).toBeEnabled();
  });
});
