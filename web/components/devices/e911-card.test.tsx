import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { E911Card } from "./e911-card";

/**
 * E911 卡片有两处不靠类型系统保证、又只在真实点击时才暴露的行为：
 *
 *  1. 弹窗必须在**点击的同步阶段**先开出来，等 POST 回来再改地址——
 *     顺序反了会被弹窗拦截器拦下，而且拦得静默。
 *  2. 完成状态只能靠轮询 /websheets/:id/status；跨源读不到窗口内容。
 */

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/e911", () => ({
  startE911Websheet: vi.fn(),
  getWebsheetStatus: vi.fn(),
}));

const { startE911Websheet, getWebsheetStatus } = await import(
  "@/lib/api/endpoints/e911"
);

function renderCard(available = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <E911Card deviceId="dev1" available={available} />
    </QueryClientProvider>,
  );
}

describe("E911Card", () => {
  let openSpy: ReturnType<typeof vi.fn>;
  let fakeWindow: { location: { replace: ReturnType<typeof vi.fn> }; close: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    fakeWindow = { location: { replace: vi.fn() }, close: vi.fn() };
    openSpy = vi.fn(() => fakeWindow);
    vi.stubGlobal("open", openSpy);
    vi.mocked(getWebsheetStatus).mockResolvedValue({
      id: "ws1",
      finished: false,
      expires_at: new Date(Date.now() + 600_000).toISOString(),
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it("运营商未启用时禁用入口并说明原因", () => {
    renderCard(false);
    expect(screen.getByText(/未启用 E911 登记/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /登记紧急地址/ })).toBeDisabled();
  });

  // 顺序是关键：open 必须发生在 await 之前，否则浏览器不认为它源于用户手势。
  it("先开空白窗口，拿到 embedUrl 后再改地址", async () => {
    let resolveStart: (v: unknown) => void = () => {};
    vi.mocked(startE911Websheet).mockReturnValue(
      new Promise((r) => {
        resolveStart = r;
      }) as never,
    );

    renderCard();
    await userEvent.click(screen.getByRole("button", { name: /登记紧急地址/ }));

    // POST 还没回来，窗口已经开了
    expect(openSpy).toHaveBeenCalledWith("about:blank", "_blank");
    expect(fakeWindow.location.replace).not.toHaveBeenCalled();

    resolveStart({
      id: "ws1",
      embedUrl: "/api/websheets/ws1?token=abc",
      url: "https://carrier.example.com/",
      method: "GET",
    });

    await waitFor(() =>
      expect(fakeWindow.location.replace).toHaveBeenCalledWith(
        "/api/websheets/ws1?token=abc",
      ),
    );
  });

  it("窗口被拦截时给出二次打开的入口", async () => {
    openSpy.mockReturnValue(null);
    vi.mocked(startE911Websheet).mockResolvedValue({
      id: "ws1",
      embedUrl: "/api/websheets/ws1?token=abc",
      url: "https://carrier.example.com/",
      method: "GET",
    });

    renderCard();
    await userEvent.click(screen.getByRole("button", { name: /登记紧急地址/ }));

    await waitFor(() =>
      expect(screen.getByText(/浏览器拦截了新窗口/)).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("button", { name: /重新打开窗口/ }),
    ).toBeInTheDocument();
  });

  it("轮询到 finished 后展示完成态", async () => {
    vi.mocked(startE911Websheet).mockResolvedValue({
      id: "ws1",
      embedUrl: "/api/websheets/ws1?token=abc",
      url: "https://carrier.example.com/",
      method: "GET",
    });
    vi.mocked(getWebsheetStatus).mockResolvedValue({
      id: "ws1",
      finished: true,
      event: "finishFlow",
      result_code: "0",
      expires_at: new Date(Date.now() + 600_000).toISOString(),
    });

    renderCard();
    await userEvent.click(screen.getByRole("button", { name: /登记紧急地址/ }));

    await waitFor(() =>
      expect(screen.getByText(/登记流程已完成/)).toBeInTheDocument(),
    );
    // 完成后窗口应被关掉，不留一个已经没用的标签页
    expect(fakeWindow.close).toHaveBeenCalled();
  });

  it("发起失败时不留在等待态", async () => {
    vi.mocked(startE911Websheet).mockRejectedValue(new Error("boom"));

    renderCard();
    await userEvent.click(screen.getByRole("button", { name: /登记紧急地址/ }));

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: /登记紧急地址/ }),
      ).toBeEnabled(),
    );
    // 开出来的空白窗口必须收掉，否则用户会盯着一个永远空白的标签页
    expect(fakeWindow.close).toHaveBeenCalled();
  });
});
