import { api } from "../client";

/**
 * E911 紧急地址登记。
 *
 * 流程由运营商的 websheet（承载表单）完成，后端只是代理它并注入一段桥接脚本：
 *
 *   1. POST .../e911/websheet 拿到一次性会话 {id, embedUrl}
 *   2. 浏览器打开 embedUrl —— 页面内容来自运营商，我们无法控制
 *   3. 轮询 /websheets/:id/status 等桥接脚本回调
 *
 * 第 3 步不能用别的办法替代：页面是跨源的，`window.opener` 读不到内容，
 * 窗口关闭事件也拿不到（`closed` 只在同源或用户手动关闭时可靠）。
 * 完成信号只有服务端知道。
 */

export interface WebsheetInfo {
  id: string;
  /** 承载页地址，已带会话自己的一次性 token，可直接打开 */
  embedUrl: string;
  title?: string;
  /** 运营商目标地址，仅用于展示"将要访问哪里" */
  url: string;
  method: string;
}

export interface WebsheetStatus {
  id: string;
  finished: boolean;
  /** 桥接脚本上报的最后一个事件名，如 finishFlow */
  event?: string;
  result_code?: string;
  title?: string;
  expires_at: string;
}

/** POST /devices/:id/vowifi/e911/websheet —— 201 + 会话信息 */
export async function startE911Websheet(
  deviceId: string,
): Promise<WebsheetInfo> {
  return api.post<WebsheetInfo>(
    `/devices/${encodeURIComponent(deviceId)}/vowifi/e911/websheet`,
    undefined,
    // 要先与运营商做一次 entitlement 交互（含 AKA 鉴权），比普通请求慢得多
    { timeoutMs: 60_000 },
  );
}

/** GET /websheets/:id/status —— 会话过期后返回 410。 */
export async function getWebsheetStatus(id: string): Promise<WebsheetStatus> {
  return api.get<WebsheetStatus>(`/websheets/${encodeURIComponent(id)}/status`);
}
