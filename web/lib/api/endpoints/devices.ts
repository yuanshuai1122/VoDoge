import { api } from "../client";
import type { DeviceOverview, DeviceListResult } from "../../../types/device";
import type {
  DeviceConfigDTO,
  DiscoveredDevice,
} from "../../../types/device-config";

/**
 * GET /api/devices
 *
 * 设备列表是 data；额度上限描述的是这批数据而非某台设备，因此在 meta 里。
 */
export async function listDevices(): Promise<DeviceListResult> {
  const { data, meta } = await api.get<DeviceOverview[]>("/devices");
  return {
    devices: data,
    device_limit:
      typeof meta.device_limit === "number" ? meta.device_limit : undefined,
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
  return (await api.get<DashboardDevice[]>("/dashboard/devices")).data;
}

/**
 * GET /api/devices/:id/overview —— 单个设备对象。
 *
 * 曾经返回 {devices:[单元素]}，设备不存在时给空数组，逼得前端自己把"空"
 * 翻译成 404。现在后端直接给对象，不存在就是 404。
 */
export async function getDeviceOverview(id: string): Promise<DeviceOverview> {
  return (
    await api.get<DeviceOverview>(`/devices/${encodeURIComponent(id)}/overview`)
  ).data;
}

/**
 * GET /api/devices/discovered?with_imei=1 -> {devices}
 *
 * `with_imei=1` 不能省。后端把**整个** IMEI 探测（AT 口 + QMI 兜底）关在这个
 * 开关后面，不带它拿回来的每一台设备 IMEI 都是空的，前端据此判定 degraded，
 * 显示「身份不可确立」并禁用添加按钮——也就是任何模组都加不进来。
 *
 * 本函数唯一的调用方是「添加设备」对话框，而它没有 IMEI 就没法工作，
 * 所以这里固定带上。探测要打串口和 QMI，故有下面这个 60s 超时。
 */
export async function listDiscoveredDevices(): Promise<DiscoveredDevice[]> {
  return (
    await api.get<DiscoveredDevice[]>("/devices/discovered", {
      query: { with_imei: "1" },
      timeoutMs: 60_000,
    })
  ).data;
}

/**
 * POST /api/devices
 * 请求体是 {config: {...}}，不是裸配置。
 * 成功响应带 started / requires_restart / warning：
 * 配置写入成功但运行时启动失败也会返回 200，warning 必须展示。
 */
export interface AddDeviceResult {
  started?: boolean;
  requires_restart?: boolean;
  warning?: string;
}

export async function addDeviceWithConfig(
  config: DeviceConfigDTO,
): Promise<AddDeviceResult> {
  // 添加设备没有资源可返回（data 为 null）；started/requires_restart/warning
  // 描述的是这次操作的结果，都在 meta 里。
  const { meta } = await api.post("/devices", { config }, { timeoutMs: 60_000 });
  return {
    started: typeof meta.started === "boolean" ? meta.started : undefined,
    requires_restart:
      typeof meta.requires_restart === "boolean"
        ? meta.requires_restart
        : undefined,
    warning: typeof meta.warning === "string" ? meta.warning : undefined,
  };
}

/** GET /api/devices/:id/config -> {config} */
export async function getDeviceConfig(id: string): Promise<DeviceConfigDTO> {
  return (await api.get<DeviceConfigDTO>(`/devices/${encodeURIComponent(id)}/config`)).data;
}

export interface UpdateDeviceResult {
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
  // 同 addDeviceWithConfig：保存本身没有资源可返回，结果说明全在 meta
  const { meta } = await api.put(
    `/devices/${encodeURIComponent(id)}`,
    { config },
    { timeoutMs: 60_000 },
  );
  return {
    requires_restart:
      typeof meta.requires_restart === "boolean"
        ? meta.requires_restart
        : undefined,
    warning: typeof meta.warning === "string" ? meta.warning : undefined,
    vowifi_error:
      typeof meta.vowifi_error === "string" ? meta.vowifi_error : undefined,
  };
}

export async function deleteDevice(id: string): Promise<void> {
  await api.delete(`/devices/${encodeURIComponent(id)}`);
}

export async function refreshDevice(id: string): Promise<void> {
  await api.post(`/devices/${encodeURIComponent(id)}/actions/refresh`);
}

export async function rebootDevice(id: string): Promise<void> {
  await api.post(`/devices/${encodeURIComponent(id)}/actions/reboot`);
}

export async function rescanDevices(): Promise<void> {
  await api.post("/devices/actions/rescan");
}

/**
 * POST /api/devices/:id/actions/at —— data 就是模组的原始回显。
 * 请求字段是 `cmd`（注意 USSD 用的是 `command`，两者不一致）。
 */
export async function executeAT(
  id: string,
  cmd: string,
  timeoutMs?: number,
): Promise<string> {
  return (
    await api.post<string>(
      `/devices/${encodeURIComponent(id)}/actions/at`,
      { cmd, timeout_ms: timeoutMs },
      { timeoutMs: 60_000 },
    )
  ).data;
}

/**
 * USSD 是多轮会话：execute -> (continue)* -> cancel。
 *
 * data 是模组返回的结果（含 session_id，续轮时必须回传）；
 * 走的是哪条通路（vowifi / cs）属于 meta.channel。
 */
export interface USSDResponse {
  session_id?: string;
  text?: string;
  [key: string]: unknown;
}

export interface USSDResult {
  /** "vowifi" 或 "cs" */
  channel: string;
  result: USSDResponse;
}

async function ussdCall(
  path: string,
  body: unknown,
): Promise<USSDResult> {
  const { data, meta } = await api.post<USSDResponse>(path, body, {
    timeoutMs: 130_000,
  });
  return {
    channel: typeof meta.channel === "string" ? meta.channel : "",
    result: data,
  };
}

export async function executeUSSD(
  id: string,
  command: string,
): Promise<USSDResult> {
  return ussdCall(`/devices/${encodeURIComponent(id)}/actions/ussd`, {
    command,
  });
}

export async function continueUSSD(
  id: string,
  sessionId: string,
  input: string,
): Promise<USSDResult> {
  return ussdCall(`/devices/${encodeURIComponent(id)}/actions/ussd/continue`, {
    session_id: sessionId,
    input,
  });
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
  await api.patch(`/devices/${encodeURIComponent(id)}/network`, input);
}

export async function setFlightMode(id: string, enabled: boolean): Promise<void> {
  await api.patch(`/devices/${encodeURIComponent(id)}/flight-mode`, { enabled });
}

export async function setVoWiFi(id: string, enabled: boolean): Promise<void> {
  await api.patch(`/devices/${encodeURIComponent(id)}/vowifi`, { enabled });
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
  await api.patch(`/devices/${encodeURIComponent(id)}/usbnet-mode`, { mode });
}

/**
 * POST /api/devices/:id/vowifi/actions/reconnect
 * 重新发起 IMS 注册。注册过程可能持续数十秒，超时放宽。
 */
export async function reconnectVoWiFi(id: string): Promise<void> {
  await api.post(
      `/devices/${encodeURIComponent(id)}/vowifi/actions/reconnect`,
      undefined,
      { timeoutMs: 90_000 },
    );
}
