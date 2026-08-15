import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ProfileBindings } from "./profile-bindings";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/proxy", () => ({
  listProfileBindings: vi.fn(),
  listUpstreamProxies: vi.fn(),
  createProfileBindings: vi.fn(),
  deleteProfileBindings: vi.fn(),
}));

vi.mock("@/lib/api/endpoints/devices", () => ({
  listDevices: vi.fn(),
}));

vi.mock("@/lib/api/endpoints/esim", () => ({
  listProfiles: vi.fn(),
}));

const { listProfileBindings, listUpstreamProxies, deleteProfileBindings } =
  await import("@/lib/api/endpoints/proxy");
const { listDevices } = await import("@/lib/api/endpoints/devices");

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ProfileBindings />
    </QueryClientProvider>,
  );
}

describe("ProfileBindings", () => {
  beforeEach(() => {
    vi.mocked(listUpstreamProxies).mockResolvedValue([
      {
        id: "route-1",
        name: "伦敦",
        addr: "127.0.0.1:1080",
        username: "",
        enabled: true,
      },
    ]);
    vi.mocked(listDevices).mockResolvedValue({ devices: [] });
    vi.mocked(deleteProfileBindings).mockResolvedValue();
  });
  afterEach(() => vi.clearAllMocks());

  it("列出已有绑定并可解除", async () => {
    vi.mocked(listProfileBindings).mockResolvedValue([
      {
        iccid: "89441000400128014257",
        device_id: "ec20",
        profile_name: "Vodafone UK",
        upstream_proxy_id: "route-1",
      },
    ]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    renderPanel();

    expect(await screen.findByText("Vodafone UK")).toBeInTheDocument();
    expect(screen.getByText("89441000400128014257")).toBeInTheDocument();
    expect(screen.getByText("伦敦")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "解除绑定" }));
    await waitFor(() => {
      expect(deleteProfileBindings).toHaveBeenCalledWith({
        upstream_proxy_id: "route-1",
        iccids: ["89441000400128014257"],
      });
    });
  });

  it("没有绑定时显示空状态", async () => {
    vi.mocked(listProfileBindings).mockResolvedValue([]);
    renderPanel();
    expect(await screen.findByText(/尚未绑定 SIM \/ Profile/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存绑定" })).toBeDisabled();
  });
});
