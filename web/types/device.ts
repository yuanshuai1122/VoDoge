/**
 * 设备相关 DTO。字段对齐 internal/api/device_mgmt.go 的 deviceMgmtOverviewLiteItem
 * 与 internal/modem 的 DeviceStatus。**不要从 OpenAPI 生成**——spec 缺 17 个真实端点。
 */

/** 设备生命周期阶段，取值见 internal/device/lifecycle.go。 */
export type LifecyclePhase =
  | "offline"
  | "rebooting"
  | "usb_wait"
  | "worker_starting"
  | "qmi_starting"
  | "recovering"
  | "online"
  | "degraded"
  | "evicting";

export const LIFECYCLE_PHASES: LifecyclePhase[] = [
  "offline",
  "rebooting",
  "usb_wait",
  "worker_starting",
  "qmi_starting",
  "recovering",
  "online",
  "degraded",
  "evicting",
];

export interface ModemStatus {
  imei: string;
  firmware: string;
  iccid: string;
  imsi: string;
  native_spn?: string;
  native_mcc?: string;
  native_mnc?: string;
  gid1?: string;
  gid2?: string;
  operator: string;
  sim_inserted: boolean;
  signal_dbm: number;
  signal_rsrp: number;
  signal_rsrq: number;
  signal_sinr?: number;
  nr5g_signal_sinr?: number;
  radio_band?: string;
  radio_channel?: number;
  reg_status: number;
  reg_status_text: string;
  ps_attached: boolean;
  lac: string;
  cell_id: string;
  apn: string;
  ims_status: number;
  network_mode: string;
  network_duplex: string;
  usbnet_mode: number;
  operating_mode?: number;
}

export interface DeviceTrafficMeta {
  [key: string]: unknown;
}

export interface VoWiFiRuntime {
  [key: string]: unknown;
}

/** GET /devices、/devices/:id/overview 以及 overview SSE 的元素类型。 */
export interface DeviceOverview {
  id: string;
  name: string;
  running: boolean;
  healthy: boolean;
  control_online: boolean;
  physical_present: boolean;
  worker_running: boolean;
  data_connected: boolean;
  radio_registered: boolean;
  lifecycle_phase: LifecyclePhase;
  lifecycle_reason?: string;
  private_ip?: string;
  private_ipv6?: string;
  public_ip: string;
  public_ipv6?: string;
  interface?: string;
  control_device?: string;
  esim_transport?: string;
  at_port?: string;
  usb_path?: string;
  audio_device?: string;
  local_phone?: string;
  e911_setup_available?: boolean;
  active_esim_profile_name?: string;
  sms_enabled: boolean;
  network_enabled: boolean;
  vowifi_enabled: boolean;
  vowifi_active: boolean;
  vowifi_runtime?: VoWiFiRuntime;
  radio_live_ok?: boolean;
  modem: ModemStatus;
  traffic?: Record<string, string>;
  traffic_raw?: Record<string, number>;
  traffic_meta?: DeviceTrafficMeta;
  backend_mode: string;
  network_connected: boolean;
  registration_state_label: string;
}

/** GET /devices 的完整响应（含配额）。 */
export interface DeviceListResult {
  devices: DeviceOverview[];
  device_limit?: number;
}

export interface DiscoveredDevice {
  [key: string]: unknown;
}
