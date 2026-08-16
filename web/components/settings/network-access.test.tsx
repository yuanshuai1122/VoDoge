import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NetworkAccessCard } from "./network-access";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/system", () => ({
  getSecuritySettings: vi.fn(),
  updateSecuritySettings: vi.fn(),
}));

const { getSecuritySettings, updateSecuritySettings } = await import(
  "@/lib/api/endpoints/system"
);

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>
      <NetworkAccessCard />
    </QueryClientProvider>,
  );
  return { ...view, client };
}

describe("NetworkAccessCard", () => {
  beforeEach(() => {
    vi.mocked(getSecuritySettings).mockResolvedValue({
      mode: "internal",
      allowed_cidrs: [],
      trust_proxy_headers: false,
      client_ip: "192.168.2.20",
      client_allowed: true,
    });
    vi.mocked(updateSecuritySettings).mockResolvedValue({
      mode: "public",
      allowed_cidrs: [],
      trust_proxy_headers: false,
      client_ip: "192.168.2.20",
      client_allowed: true,
    });
  });
  afterEach(() => vi.clearAllMocks());

  it("可以切到对公网开放并保存", async () => {
    renderCard();
    expect(await screen.findByText(/当前连接 192.168.2.20/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "对公网开放" }));
    await userEvent.click(screen.getByRole("button", { name: "保存访问策略" }));
    await waitFor(() => {
      expect(updateSecuritySettings).toHaveBeenCalledWith({
        mode: "public",
        allowed_cidrs: [],
        trust_proxy_headers: false,
      });
    });
  });

  it("查询刷新不会覆盖正在编辑的访问策略", async () => {
    const { client } = renderCard();
    expect(await screen.findByText(/当前连接 192.168.2.20/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "对公网开放" }));
    await userEvent.click(screen.getByRole("button", { name: "添加" }));
    await userEvent.type(
      screen.getByPlaceholderText("203.0.113.0/24"),
      "203.0.113.0/24",
    );
    await userEvent.click(screen.getByRole("switch", { name: "信任代理请求头" }));

    act(() => {
      client.setQueryData(["settings", "security"], {
        mode: "internal",
        allowed_cidrs: ["10.0.0.0/8"],
        trust_proxy_headers: false,
        client_ip: "192.168.2.21",
        client_allowed: true,
      });
    });

    expect(await screen.findByText(/当前连接 192.168.2.21/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "保存访问策略" }));
    await waitFor(() => {
      expect(updateSecuritySettings).toHaveBeenCalledWith({
        mode: "public",
        allowed_cidrs: ["203.0.113.0/24"],
        trust_proxy_headers: true,
      });
    });
  });
});
