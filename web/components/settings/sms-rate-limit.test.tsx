import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SMSRateLimitCard } from "./sms-rate-limit";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/system", () => ({
  getSMSSettings: vi.fn(),
  updateSMSSettings: vi.fn(),
}));

const { getSMSSettings, updateSMSSettings } = await import(
  "@/lib/api/endpoints/system"
);

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <SMSRateLimitCard />
    </QueryClientProvider>,
  );
}

describe("SMSRateLimitCard", () => {
  beforeEach(() => {
    vi.mocked(getSMSSettings).mockResolvedValue({
      hourly_limit: 20,
      used: 3,
      remaining: 17,
      window_seconds: 3600,
      unlimited: false,
    });
    vi.mocked(updateSMSSettings).mockResolvedValue({
      hourly_limit: 8,
      used: 3,
      remaining: 5,
      window_seconds: 3600,
      unlimited: false,
    });
  });
  afterEach(() => vi.clearAllMocks());

  it("展示当前用量并保存新限额", async () => {
    renderCard();
    expect(await screen.findByText(/已发送 3 \/ 20 条/)).toBeInTheDocument();

    const input = screen.getByLabelText(/每小时上限/);
    await userEvent.clear(input);
    await userEvent.type(input, "8");
    await userEvent.click(screen.getByRole("button", { name: "保存限额" }));

    await waitFor(() => {
      expect(updateSMSSettings).toHaveBeenCalledWith({ hourly_limit: 8 });
    });
  });

  it("超出范围时不能提交", async () => {
    renderCard();
    const input = await screen.findByLabelText(/每小时上限/);
    await userEvent.clear(input);
    await userEvent.type(input, "201");
    expect(screen.getByRole("button", { name: "保存限额" })).toBeDisabled();
    expect(updateSMSSettings).not.toHaveBeenCalled();
  });
});
