import { api } from "../client";
import { ok, raw, rawArray } from "../unwrap";

/** 本机代理实例。对齐 internal/api/proxy.go 的 proxyInstanceDTO。 */
export interface ProxyInstance {
  id: string;
  name: string;
  device_id: string;
  enabled: boolean;
  /** socks5 | http */
  mode: string;
  listen_addr: string;
  listen_port: number;
  auth_enabled: boolean;
  username: string;
  password?: string;
}

/** 运行态。对齐 internal/proxy/server.InstanceStatus。 */
export interface ProxyInstanceStatus {
  id: string;
  mode?: string;
  running: boolean;
  started_at?: string;
  last_exit_at?: string;
  last_exit_ok?: boolean;
  last_error?: string;
  listen_addr?: string;
  listen_port?: number;
  interface?: string;
  auth_enabled?: boolean;
}

export interface ProxyDevice {
  id: string;
  name: string;
  interface: string;
}

export interface ProxyOverview {
  instances: ProxyInstance[];
  devices: ProxyDevice[];
  status: ProxyInstanceStatus[];
}

/** GET /api/proxy-instances/overview -> {instances, devices, status} */
export async function getProxyOverview(): Promise<ProxyOverview> {
  const body = await api.get<ProxyOverview>("/proxy-instances/overview");
  return {
    instances: body?.instances ?? [],
    devices: body?.devices ?? [],
    status: body?.status ?? [],
  };
}

/** PUT /api/proxy-instances/config —— 整体保存实例列表 */
export async function saveProxyConfig(instances: ProxyInstance[]): Promise<void> {
  ok(await api.put("/proxy-instances/config", { instances }));
}

export async function startInstance(id: string): Promise<void> {
  ok(await api.post(`/proxy-instances/${encodeURIComponent(id)}/actions/start`));
}

export async function stopInstance(id: string): Promise<void> {
  ok(await api.post(`/proxy-instances/${encodeURIComponent(id)}/actions/stop`));
}

export async function restartInstance(id: string): Promise<void> {
  ok(
    await api.post(`/proxy-instances/${encodeURIComponent(id)}/actions/restart`),
  );
}

/** 上游代理。对齐 internal/db.UpstreamProxy。密码在列表接口中已被后端脱敏。 */
export interface UpstreamProxy {
  id: string;
  name: string;
  addr: string;
  username: string;
  password?: string;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

/** GET /api/upstream-proxies —— 裸数组 */
export async function listUpstreamProxies(): Promise<UpstreamProxy[]> {
  return rawArray<UpstreamProxy>(await api.get("/upstream-proxies"));
}

export async function createUpstreamProxy(
  input: Partial<UpstreamProxy>,
): Promise<void> {
  await api.post("/upstream-proxies", input);
}

export async function updateUpstreamProxy(
  id: string,
  input: Partial<UpstreamProxy>,
): Promise<void> {
  await api.put(`/upstream-proxies/${encodeURIComponent(id)}`, input);
}

export async function deleteUpstreamProxy(id: string): Promise<void> {
  await api.delete(`/upstream-proxies/${encodeURIComponent(id)}`);
}

export interface ProbeResult {
  [key: string]: unknown;
}

export async function probeUpstreamProxy(id: string): Promise<ProbeResult> {
  return raw<ProbeResult>(
    await api.post(
      `/upstream-proxies/${encodeURIComponent(id)}/actions/probe`,
      undefined,
      { timeoutMs: 60_000 },
    ),
  );
}

export async function listUpstreamCountries(): Promise<unknown> {
  return api.get("/upstream-proxy-countries");
}

export async function listCountryRules(): Promise<unknown> {
  return api.get("/upstream-proxy-country-rules");
}

export async function upsertCountryRule(
  code: string,
  input: unknown,
): Promise<void> {
  await api.put(
    `/upstream-proxy-country-rules/${encodeURIComponent(code)}`,
    input,
  );
}

export async function deleteCountryRule(code: string): Promise<void> {
  await api.delete(
    `/upstream-proxy-country-rules/${encodeURIComponent(code)}`,
  );
}
