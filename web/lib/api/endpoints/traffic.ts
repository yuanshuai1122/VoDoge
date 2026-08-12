import { api } from "../client";

/** 对齐 internal/db.TrafficBucket */
export interface TrafficBucket {
  bucket: string;
  period_start: string;
  rx_bytes: number;
  tx_bytes: number;
  total_bytes: number;
}

/** 对齐 internal/db.TrafficChartData */
export interface TrafficChartData {
  timestamps: string[];
  period_starts: string[];
  devices: string[];
  /** device_id -> 每个时间点的字节数 */
  series: Record<string, number[]>;
}

export interface TrafficAnalysis {
  status: string;
  range: string;
  buckets: TrafficBucket[];
  chart: TrafficChartData | null;
}

export type TrafficRange = "day" | "week" | "month";

/** GET /api/traffic/analysis -> {status, range, buckets, chart} */
export async function getTrafficAnalysis(params: {
  range?: TrafficRange;
  deviceId?: string;
}): Promise<TrafficAnalysis> {
  const body = await api.get<TrafficAnalysis>("/traffic/analysis", {
    query: { range: params.range ?? "day", device_id: params.deviceId },
  });
  return {
    status: body?.status ?? "ok",
    range: body?.range ?? "day",
    buckets: body?.buckets ?? [],
    chart: body?.chart ?? null,
  };
}
