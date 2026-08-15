import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DeviceQuotaCard } from "./device-quota";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/system", () => ({
  getDeviceQuota: vi.fn(),
  updateDeviceQuota: vi.fn(),
}));

const { getDeviceQuota, updateDeviceQuota } = await import(
  "@/lib/api/endpoints/system"
);

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <DeviceQuotaCard />
    </QueryClientProvider>,
  );
}

describe("DeviceQuotaCard", () => {
  beforeEach(() => {
    vi.mocked(getDeviceQuota).mockResolvedValue({
      limit: 5,
      used: 2,
      default_limit: 5,
      max_limit: 10,
    });
    vi.mocked(updateDeviceQuota).mockResolvedValue({
      limit: 8,
      used: 2,
      default_limit: 5,
      max_limit: 10,
    });
  });
  afterEach(() => vi.clearAllMocks());

  it("展示用量并保存新配额", async () => {
    renderCard();
    expect(await screen.findByText(/已配置 2 \/ 5 台/)).toBeInTheDocument();

    const input = screen.getByLabelText(/最多设备数/);
    await userEvent.clear(input);
    await userEvent.type(input, "8");
    await userEvent.click(screen.getByRole("button", { name: "保存配额" }));

    await waitFor(() => {
      expect(updateDeviceQuota).toHaveBeenCalledWith({ limit: 8 });
    });
  });

  it("超出范围时不能提交", async () => {
    renderCard();
    const input = await screen.findByLabelText(/最多设备数/);
    await userEvent.clear(input);
    await userEvent.type(input, "11");
    expect(screen.getByRole("button", { name: "保存配额" })).toBeDisabled();
    expect(updateDeviceQuota).not.toHaveBeenCalled();
  });
});
