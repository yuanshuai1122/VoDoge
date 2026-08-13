import { api } from "../client";
import { ok, pick, pickOr, pickFirstDevice, rawArray } from "../unwrap";
import type { DeviceOverview, DeviceListResult } from "../../../types/device";
import type {
  DeviceConfigDTO,
  DiscoveredDevice,
} from "../../../types/device-config";

/** GET /api/devices -> {devices, device_limit} */
export async function listDevices(): Promise<DeviceListResult> {
  const body = await api.get("/devices");
  return {
    devices: pick<DeviceOverview[]>(body, "devices"),
    device_limit: pickOr<number | undefined>(body, "device_limit", undefined),
  };
}

/**
 * GET /api/dashboard/devices —— **裸数组**，且元素类型与 /devices 不同。
 *
 * 后端这里走的是缓存快照（handleListDevices，注释写明「0 IPC」），字段是精简过的：
 * **没有** lifecycle_phase、data_connected、physical_present、modem 等。
 * 需要生命周期状态或模组明细时必须用 listDevices()，否则统计会恒为 0。
 */
export interface DashboardDevice {
  id: string;
  name: string;
  interface: string;
  proxy_port: number;
  public_ip: string;
  public_ipv6?: string;
  healthy: boolean;
  operator: string;
  signal_dbm: number;
  network_mode: string;
  network_duplex: string;
  vowifi_active: boolean;
  traffic?: Record<string, string>;
  network_connected: boolean;
}

export async function listDashboardDevices(): Promise<DashboardDevice[]> {
  return rawArray<DashboardDevice>(await api.get("/dashboard/devices"));
}

/**
 * GET /api/devices/:id/overview
 * 注意后端返回的是 {devices:[单元素]}，不是单对象。
 */
export async function getDeviceOverview(id: string): Promise<DeviceOverview> {
  return pickFirstDevice<DeviceOverview>(
    await api.get(`/devices/${encodeURIComponent(id)}/overview`),
  );
}

/** GET /api/devices/discovered -> {devices} */
export async function listDiscoveredDevices(): Promise<DiscoveredDevice[]> {
  return pick<DiscoveredDevice[]>(
    await api.get("/devices/discovered", { timeoutMs: 60_000 }),
    "devices",
  );
}

/**
 * POST /api/devices
 * 请求体是 {config: {...}}，不是裸配置。
 * 成功响应带 started / requires_restart / warning：
 * 配置写入成功但运行时启动失败也会返回 200，warning 必须展示。
 */
export interface AddDeviceResult {
  status: string;
  started?: boolean;
  requires_restart?: boolean;
  warning?: string;
}

export async function addDeviceWithConfig(
  config: DeviceConfigDTO,
): Promise<AddDeviceResult> {
  return api.post<AddDeviceResult>("/devices", { config }, { timeoutMs: 60_000 });
}

/** GET /api/devices/:id/config -> {config} */
export async function getDeviceConfig(id: string): Promise<DeviceConfigDTO> {
  return pick<DeviceConfigDTO>(
    await api.get(`/devices/${encodeURIComponent(id)}/config`),
    "config",
  );
}

export interface UpdateDeviceResult {
  status: string;
  requires_restart?: boolean;
  warning?: string;
  vowifi_error?: string;
}

/**
 * PUT /api/devices/:id —— 请求体是 {config: {...}}。
 *
 * **整体替换，不是增量合并**：除 QMI 的三个指针字段会从现有配置继承外，
 * 其余字段一律采信请求体，漏传即被清空。因此必须基于 getDeviceConfig 的
 * 完整结果修改后整体提交。
 *
 * 策略字段（network_enabled / vowifi_enabled / apn / ip_version）是例外：
 * 后端会用「当前有效策略」覆盖请求体中的值，因为 GET config 并不投影它们
 * （恒为零值），直接采信会把卡策略清空。策略请改用 PUT /cards/:iccid/policy。
 */
export async function updateDevice(
  id: string,
  config: DeviceConfigDTO,
): Promise<UpdateDeviceResult> {
  return api.put<UpdateDeviceResult>(
    `/devices/${encodeURIComponent(id)}`,
    { config },
    { timeoutMs: 60_000 },
  );
}

export async function deleteDevice(id: string): Promise<void> {
  ok(await api.delete(`/devices/${encodeURIComponent(id)}`));
}

export async function refreshDevice(id: string): Promise<void> {
  ok(await api.post(`/devices/${encodeURIComponent(id)}/actions/refresh`));
}

export async function rebootDevice(id: string): Promise<void> {
  ok(await api.post(`/devices/${encodeURIComponent(id)}/actions/reboot`));
}

export async function rescanDevices(): Promise<void> {
  ok(await api.post("/devices/actions/rescan"));
}

/**
 * POST /api/devices/:id/actions/at -> {status:"ok", response}
 * 请求字段是 `cmd`（注意 USSD 用的是 `command`，两者不一致）。
 */
export async function executeAT(
  id: string,
  cmd: string,
  timeoutMs?: number,
): Promise<string> {
  const body = await api.post(
    `/devices/${encodeURIComponent(id)}/actions/at`,
    { cmd, timeout_ms: timeoutMs },
    { timeoutMs: 60_000 },
  );
  return pick<string>(body, "response");
}

/**
 * USSD 是多轮会话：execute -> (continue)* -> cancel。
 * 响应形如 {status:"ok", result, channel}，channel 为 "vowifi" 或 "cs"。
 * result 中携带 session_id，续轮时必须回传。
 */
export interface USSDResult {
  status: string;
  channel: string;
  result: {
    session_id?: string;
    text?: string;
    [key: string]: unknown;
  };
}

export async function executeUSSD(
  id: string,
  command: string,
): Promise<USSDResult> {
  return api.post<USSDResult>(
    `/devices/${encodeURIComponent(id)}/actions/ussd`,
    { command },
    { timeoutMs: 130_000 },
  );
}

export async function continueUSSD(
  id: string,
  sessionId: string,
  input: string,
): Promise<USSDResult> {
  return api.post<USSDResult>(
    `/devices/${encodeURIComponent(id)}/actions/ussd/continue`,
    { session_id: sessionId, input },
    { timeoutMs: 130_000 },
  );
}

export async function cancelUSSD(id: string, sessionId?: string): Promise<void> {
  await api.post(
    `/devices/${encodeURIComponent(id)}/actions/ussd/cancel`,
    { session_id: sessionId ?? "" },
  );
}

/** PATCH /api/devices/:id/network，enabled 必填 */
export async function setDeviceNetwork(
  id: string,
  input: { enabled: boolean; ip_version?: string; apn?: string },
): Promise<void> {
  ok(await api.patch(`/devices/${encodeURIComponent(id)}/network`, input));
}

export async function setFlightMode(id: string, enabled: boolean): Promise<void> {
  ok(await api.patch(`/devices/${encodeURIComponent(id)}/flight-mode`, { enabled }));
}

export async function setVoWiFi(id: string, enabled: boolean): Promise<void> {
  ok(await api.patch(`/devices/${encodeURIComponent(id)}/vowifi`, { enabled }));
}

/**
 * USBNET 模式。决定模组以哪种网络设备形态挂到主机上（RMNET/ECM/MBIM/RNDIS/NCM）。
 *
 * 三点要注意：
 *  - 仅 Quectel 模组支持（后端下发 `AT+QCFG="usbnet",N`），且需要 AT 端口；
 *    纯 QMI 接管的设备会返回 400。
 *  - **改完模组会立即重启**（`AT+CFUN=1,1`），随后由不同的内核驱动接管，
 *    控制节点与网卡名都会变——设备会先掉线再以新形态出现。
 *  - 因此这是一次性动作，不属于「保存配置」，UI 上必须单独确认。
 */
export const USBNET_MODES = [
  { value: 0, label: "RMNET / QMI", hint: "Qualcomm 私有，配合 qmi_wwan 驱动" },
  { value: 1, label: "ECM", hint: "标准 CDC-ECM，通用性好" },
  { value: 2, label: "MBIM", hint: "标准 CDC-MBIM" },
  { value: 3, label: "RNDIS", hint: "主要面向 Windows" },
  { value: 5, label: "NCM", hint: "CDC-NCM，部分型号支持" },
] as const;

export async function setUSBNetMode(id: string, mode: number): Promise<void> {
  ok(
    await api.patch(`/devices/${encodeURIComponent(id)}/usbnet-mode`, { mode }),
  );
}

/**
 * POST /api/devices/:id/vowifi/actions/reconnect
 * 重新发起 IMS 注册。注册过程可能持续数十秒，超时放宽。
 */
export async function reconnectVoWiFi(id: string): Promise<void> {
  ok(
    await api.post(
      `/devices/${encodeURIComponent(id)}/vowifi/actions/reconnect`,
      undefined,
      { timeoutMs: 90_000 },
    ),
  );
}
