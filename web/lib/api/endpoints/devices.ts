import { api } from "../client";
import { ok, pick, pickOr, pickFirstDevice } from "../unwrap";
import type {
  DeviceOverview,
  DeviceListResult,
  DiscoveredDevice,
} from "../../../types/device";

/** GET /api/devices -> {devices, device_limit} */
export async function listDevices(): Promise<DeviceListResult> {
  const body = await api.get("/devices");
  return {
    devices: pick<DeviceOverview[]>(body, "devices"),
    device_limit: pickOr<number | undefined>(body, "device_limit", undefined),
  };
}

/** GET /api/dashboard/devices -> {devices} */
export async function listDashboardDevices(): Promise<DeviceOverview[]> {
  return pick<DeviceOverview[]>(await api.get("/dashboard/devices"), "devices");
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
    await api.get("/devices/discovered"),
    "devices",
  );
}

/** GET /api/devices/:id/config -> {config} */
export async function getDeviceConfig(id: string): Promise<unknown> {
  return pick(await api.get(`/devices/${encodeURIComponent(id)}/config`), "config");
}

export async function addDevice(input: unknown): Promise<void> {
  ok(await api.post("/devices", input));
}

export async function updateDevice(id: string, input: unknown): Promise<void> {
  ok(await api.put(`/devices/${encodeURIComponent(id)}`, input));
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

/** POST /api/devices/:id/actions/at -> {status:"ok", response} */
export async function executeAT(id: string, command: string): Promise<string> {
  const body = await api.post(
    `/devices/${encodeURIComponent(id)}/actions/at`,
    { command },
    { timeoutMs: 60_000 },
  );
  return pick<string>(body, "response");
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
