import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PluginsCard } from "./plugins-card";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/extensions", () => ({
  listPlugins: vi.fn(),
  installPluginURL: vi.fn(),
  updatePlugin: vi.fn(),
  uninstallPlugin: vi.fn(),
  uploadPlugin: vi.fn(),
  pluginPageURL: (id: string, c: string) =>
    `/plugins?plugin=${id}&contribution=${c}`,
}));

const {
  listPlugins,
  installPluginURL,
  updatePlugin,
} = await import("@/lib/api/endpoints/extensions");

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <PluginsCard />
    </QueryClientProvider>,
  );
}

describe("PluginsCard", () => {
  beforeEach(() => {
    vi.mocked(listPlugins).mockResolvedValue([
      {
        id: "hello-lab",
        name: "Hello",
        version: "1.0.0",
        contributions: [
          {
            id: "hello-page",
            label: "Hello",
            location: "sidebar",
            entry: "index.html",
          },
        ],
        enabled: true,
        backend_available: false,
        backend_running: false,
        installed_at: "2026-08-15T00:00:00Z",
        sha256: "abc",
      },
    ]);
    vi.stubGlobal("confirm", vi.fn(() => true));
  });
  afterEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it("列出插件并允许从 URL 安装", async () => {
    vi.mocked(installPluginURL).mockResolvedValue({
      id: "other",
      name: "Other",
      version: "1",
      contributions: [],
      enabled: true,
      backend_available: false,
      backend_running: false,
      installed_at: "",
      sha256: "",
    });
    renderCard();
    expect(await screen.findByText("Hello")).toBeInTheDocument();

    await userEvent.type(
      screen.getByLabelText(/插件包 URL/),
      "https://example.com/p.zip",
    );
    await userEvent.click(screen.getByRole("button", { name: "从 URL 安装" }));
    await waitFor(() => {
      expect(installPluginURL).toHaveBeenCalledWith({
        url: "https://example.com/p.zip",
        sha256: undefined,
      });
    });
  });

  it("可以禁用已安装插件", async () => {
    vi.mocked(updatePlugin).mockResolvedValue({
      id: "hello-lab",
      name: "Hello",
      version: "1.0.0",
      contributions: [],
      enabled: false,
      backend_available: false,
      backend_running: false,
      installed_at: "",
      sha256: "",
    });
    renderCard();
    await userEvent.click(await screen.findByRole("button", { name: "禁用" }));
    await waitFor(() => {
      expect(updatePlugin).toHaveBeenCalledWith("hello-lab", false);
    });
  });
});
