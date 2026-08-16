import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import PluginPage from "./page";

vi.mock("next/navigation", () => ({
  useSearchParams: () =>
    new URLSearchParams("plugin=hello-lab&contribution=hello-page"),
}));

vi.mock("@/lib/api/endpoints/extensions", () => ({
  listPlugins: vi.fn(),
  createPluginSession: vi.fn(),
}));

const { listPlugins, createPluginSession } = await import(
  "@/lib/api/endpoints/extensions"
);

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <PluginPage />
    </QueryClientProvider>,
  );
}

describe("PluginPage", () => {
  beforeEach(() => {
    vi.mocked(listPlugins).mockResolvedValue([
      {
        id: "hello-lab",
        name: "Hello Lab",
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
        installed_at: "2026-08-16T00:00:00Z",
        sha256: "abc",
      },
    ]);
    vi.mocked(createPluginSession).mockResolvedValue({
      launch_url:
        "http://device.local:7576/plugin-assets/hello-lab/index.html?token=capability",
      expires_at: "2026-08-16T00:30:00Z",
    });
  });

  it("用独立 origin 的短期会话启动插件 iframe", async () => {
    renderPage();

    const frame = await screen.findByTitle("Hello");
    expect(createPluginSession).toHaveBeenCalledWith(
      "hello-lab",
      "hello-page",
    );
    expect(frame).toHaveAttribute(
      "src",
      "http://device.local:7576/plugin-assets/hello-lab/index.html?token=capability",
    );
    expect(frame.getAttribute("src")).not.toMatch(/^\/plugin-assets\//);
  });
});
