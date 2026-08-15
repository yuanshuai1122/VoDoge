/** 对齐 internal/api 的 deviceConfigDTO。 */
export interface DeviceConfigDTO {
  id: string;
  name: string;
  modem_imei: string;
  usb_path: string;
  at_port: string;
  proxy_port: number;
  interface: string;
  control_device?: string;
  esim_transport?: string;
  operator_selection_mode?: string;
  operator_selection_plmn?: string;
  operator_selection_rat?: string;
  sms_enabled: boolean;
  apn?: string;
  ip_version?: string;
  network_enabled: boolean;
  vowifi_enabled: boolean;
  device_backend?: string;
  module_vendor?: string;
  /** cn | intl | 空。人工分线，不按 MCC 推断。 */
  lane?: string;
  /** PC/SC 读卡器名；device_backend=pcsc 时必填 */
  reader_name?: string;
}

/** 对齐 internal/api 的 discoveredDevice。 */
export interface DiscoveredDevice {
  discovery_key: string;
  control_path: string;
  net_interface: string;
  usb_path: string;
  imei?: string;
  vendor_id: number;
  product_id: number;
  driver_name: string;
  at_ports: string[];
  at_port: string;
  audio_device?: string;
  /** qmi/mbim/ecm/rndis/ncm/unknown */
  mode?: string;
  network_capable: boolean;
  configured: boolean;
  configured_id?: string;
  /** 探不到 IMEI，无法确立身份，不可直接添加 */
  degraded?: boolean;
}

/** 由发现结果预填一份可提交的设备配置。 */
export function configFromDiscovered(
  d: DiscoveredDevice,
  overrides: Partial<DeviceConfigDTO> = {},
): DeviceConfigDTO {
  return {
    // 设备身份以 IMEI 为准，后端也用它去重
    id: d.imei || d.discovery_key,
    name: "",
    modem_imei: d.imei ?? "",
    usb_path: d.usb_path,
    at_port: d.at_port || d.at_ports?.[0] || "",
    proxy_port: 0,
    interface: d.net_interface,
    control_device: d.control_path,
    sms_enabled: true,
    network_enabled: d.network_capable,
    vowifi_enabled: false,
    ...overrides,
  };
}
