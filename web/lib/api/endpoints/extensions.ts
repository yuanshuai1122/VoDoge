import { api, API_BASE } from "../client";
import { parseApiError } from "../errors";
import { getToken } from "../../auth/token";
import type { InstalledPlugin } from "../../../types/plugin";

export async function listPlugins(): Promise<InstalledPlugin[]> {
  return (await api.get<InstalledPlugin[]>("/extensions")).data ?? [];
}

export interface PluginSession {
  launch_url: string;
  expires_at: string;
}

export async function createPluginSession(
  pluginId: string,
  contributionId: string,
): Promise<PluginSession> {
  return (
    await api.post<PluginSession>(
      `/extensions/${encodeURIComponent(pluginId)}/session`,
      { contribution_id: contributionId },
    )
  ).data;
}

export async function installPluginURL(input: {
  url: string;
  sha256?: string;
}): Promise<InstalledPlugin> {
  return (
    await api.post<InstalledPlugin>("/extensions/install-url", {
      url: input.url,
      sha256: input.sha256 ?? "",
    })
  ).data;
}

export async function updatePlugin(
  id: string,
  enabled: boolean,
): Promise<InstalledPlugin> {
  return (
    await api.put<InstalledPlugin>(
      `/extensions/${encodeURIComponent(id)}`,
      { enabled },
    )
  ).data;
}

export async function uninstallPlugin(id: string): Promise<void> {
  await api.delete(`/extensions/${encodeURIComponent(id)}`);
}

export async function uploadPlugin(file: File): Promise<InstalledPlugin> {
  const form = new FormData();
  form.append("package", file);
  const headers = new Headers();
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`${API_BASE}/api/extensions/upload`, {
    method: "POST",
    headers,
    body: form,
  });
  let payload: unknown;
  try {
    payload = await res.json();
  } catch {
    payload = undefined;
  }
  if (!res.ok) throw parseApiError(res.status, payload);
  if (!payload || typeof payload !== "object" || !("data" in payload)) {
    throw parseApiError(res.status || 200, payload);
  }
  return (payload as { data: InstalledPlugin }).data;
}

export function pluginPageURL(pluginId: string, contributionId: string): string {
  const q = new URLSearchParams({
    plugin: pluginId,
    contribution: contributionId,
  });
  return `/plugins?${q.toString()}`;
}
