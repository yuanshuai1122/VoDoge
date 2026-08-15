import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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
  return render(
    <QueryClientProvider client={client}>
      <NetworkAccessCard />
    </QueryClientProvider>,
  );
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
});
