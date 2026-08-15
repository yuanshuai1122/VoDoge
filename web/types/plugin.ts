export interface PluginContribution {
  id: string;
  label: string;
  label_zh?: string;
  label_en?: string;
  location: "sidebar" | "proxy";
  after?: string;
  entry: string;
}

export interface InstalledPlugin {
  id: string;
  name: string;
  version: string;
  description?: string;
  author?: string;
  homepage?: string;
  permissions?: string[];
  contributions: PluginContribution[];
  enabled: boolean;
  backend_available: boolean;
  backend_running: boolean;
  backend_error?: string;
  installed_at: string;
  sha256: string;
}
