import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import SmsPage from "./page";
import type { SMSContact, SMSMessage } from "@/types/sms";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

vi.mock("@/lib/api/endpoints/sms", () => ({
  listContacts: vi.fn(),
  listThread: vi.fn(),
  sendSMS: vi.fn(),
  deleteThread: vi.fn(),
  nextContactsCursor: () => undefined,
  nextThreadCursor: () => undefined,
  SMS_PAGE_SIZE: 50,
}));

const { listContacts, listThread } = await import("@/lib/api/endpoints/sms");

const contact: SMSContact = {
  imsi: "460001",
  iccid: "8986001",
  peer: "10086",
  last_sms_id: 1,
  last_timestamp: "2026-08-15T10:00:00Z",
  last_content: "验证码 1234",
  last_type: 1,
  unread_count: 1,
  created_at: "2026-08-15T10:00:00Z",
  updated_at: "2026-08-15T10:00:00Z",
  device_id: "dev-cn",
  device_name: "国内棒",
  local_phone: "",
  lane: "cn",
};

const message: SMSMessage = {
  id: 1,
  imsi: "460001",
  iccid: "8986001",
  peer: "10086",
  local_phone: "",
  sender: "10086",
  recipient: "",
  content: "验证码 1234",
  type: 1,
  status: 1,
  timestamp: "2026-08-15T10:00:00Z",
  created_at: "2026-08-15T10:00:00Z",
  device_name: "国内棒",
};

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <SmsPage />
    </QueryClientProvider>,
  );
}

describe("SmsPage", () => {
  beforeEach(() => {
    vi.mocked(listContacts).mockReset();
    vi.mocked(listThread).mockReset();
    vi.mocked(listContacts).mockImplementation(async (params) => {
      if (params.lane === "intl") return [];
      return [contact];
    });
    vi.mocked(listThread).mockResolvedValue([message]);
  });

  it("能选会话、返回列表", async () => {
    const user = userEvent.setup();
    renderPage();

    expect(await screen.findByRole("button", { name: /10086/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("返回会话列表")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /10086/ }));

    expect(await screen.findByLabelText("返回会话列表")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("发送到 10086")).toBeInTheDocument();

    await user.click(screen.getByLabelText("返回会话列表"));

    expect(screen.queryByLabelText("返回会话列表")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /10086/ })).toBeInTheDocument();
  });

  it("全部 / 国内 / 国外会改查询参数", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("button", { name: /10086/ });

    await user.click(screen.getByRole("tab", { name: "国外" }));
    expect(listContacts).toHaveBeenCalledWith(
      expect.objectContaining({ lane: "intl" }),
    );

    await user.click(screen.getByRole("tab", { name: "国内" }));
    expect(listContacts).toHaveBeenCalledWith(
      expect.objectContaining({ lane: "cn" }),
    );

    await user.click(screen.getByRole("tab", { name: "全部" }));
    expect(listContacts).toHaveBeenCalledWith(
      expect.objectContaining({ lane: undefined }),
    );
  });
});
