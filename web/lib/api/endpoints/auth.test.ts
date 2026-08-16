import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../client", () => ({
  api: { post: vi.fn() },
}));

vi.mock("../../auth/token", () => ({
  setToken: vi.fn(),
  triggerLogout: vi.fn(),
}));

const { api } = await import("../client");
const { triggerLogout } = await import("../../auth/token");
const { logout } = await import("./auth");

describe("logout", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("清理服务端遗留 Cookie 后删除本地 Bearer", async () => {
    vi.mocked(api.post).mockResolvedValue({
      data: null,
      meta: {},
      requestId: "req-1",
    });

    await logout();

    expect(api.post).toHaveBeenCalledWith("/auth/logout", undefined, {
      skipAuthRedirect: true,
    });
    expect(triggerLogout).toHaveBeenCalledOnce();
  });

  it("服务端清理失败时仍删除本地 Bearer", async () => {
    const error = new Error("network down");
    vi.mocked(api.post).mockRejectedValue(error);

    await expect(logout()).rejects.toBe(error);
    expect(triggerLogout).toHaveBeenCalledOnce();
  });
});
