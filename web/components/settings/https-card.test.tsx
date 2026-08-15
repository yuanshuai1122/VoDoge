import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HTTPSCard } from "./https-card";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/system", () => ({
  getHTTPSSettings: vi.fn(),
  updateHTTPSSettings: vi.fn(),
  downloadHTTPSCertificate: vi.fn(),
}));

const { getHTTPSSettings, updateHTTPSSettings, downloadHTTPSCertificate } =
  await import("@/lib/api/endpoints/system");

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <HTTPSCard />
    </QueryClientProvider>,
  );
}

describe("HTTPSCard", () => {
  beforeEach(() => {
    vi.mocked(getHTTPSSettings).mockResolvedValue({
      enabled: false,
      http_url: "http://localhost:7575",
      https_url: "https://localhost:7575",
      fingerprint: "AA:BB",
    });
    vi.mocked(updateHTTPSSettings).mockResolvedValue({
      enabled: true,
      http_url: "http://localhost:7575",
      https_url: "https://localhost:7575",
      fingerprint: "AA:BB",
    });
    vi.mocked(downloadHTTPSCertificate).mockResolvedValue();
  });
  afterEach(() => vi.clearAllMocks());

  it("展示指纹并下载证书", async () => {
    renderCard();
    expect(await screen.findByText("AA:BB")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "下载自签证书" }));
    await waitFor(() => {
      expect(downloadHTTPSCertificate).toHaveBeenCalled();
    });
  });

  it("可以打开开关", async () => {
    renderCard();
    const sw = await screen.findByRole("switch", { name: "开关本机 HTTPS" });
    await userEvent.click(sw);
    await waitFor(() => {
      expect(updateHTTPSSettings).toHaveBeenCalledWith({ enabled: true });
    });
  });
});
