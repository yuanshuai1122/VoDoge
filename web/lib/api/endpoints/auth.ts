import { api } from "../client";
import { setToken, triggerLogout } from "../../auth/token";

interface LoginResponse {
  token: string;
  expires_at: string;
}

/**
 * POST /api/auth/login
 * 失败：401 invalid_credentials / 429 rate_limited（2 分钟 10 次/IP）
 */
export async function login(username: string, password: string): Promise<void> {
  const { data } = await api.post<LoginResponse>(
    "/auth/login",
    { username, password },
    { skipAuthRedirect: true },
  );
  setToken(data.token, data.expires_at);
}

/**
 * POST /api/settings/password
 *
 * 后端 token 的 HMAC 密钥就是登录密码，改密后所有既有 token 立即失效，
 * 因此这里成功后强制登出——否则界面看着已登录但每个请求都 401。
 */
export async function changePassword(input: {
  old_password: string;
  new_password: string;
  confirm_password: string;
}): Promise<void> {
  await api.post("/settings/password", input);
  triggerLogout();
}

/** 服务端无登出接口，清本地凭证即可。 */
export function logout(): void {
  triggerLogout();
}
