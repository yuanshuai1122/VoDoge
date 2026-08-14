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
  /** 请求参数的回显，来自 meta */
  range: string;
  buckets: TrafficBucket[];
  chart: TrafficChartData | null;
}

export type TrafficRange = "day" | "week" | "month";

/**
 * GET /api/traffic/analysis
 *
 * buckets 与 chart 是数据；range 只是把请求参数回显回来，属于 meta。
 */
export async function getTrafficAnalysis(params: {
  range?: TrafficRange;
  deviceId?: string;
}): Promise<TrafficAnalysis> {
  const { data, meta } = await api.get<{
    buckets?: TrafficBucket[];
    chart?: TrafficChartData | null;
  }>("/traffic/analysis", {
    query: { range: params.range ?? "day", device_id: params.deviceId },
  });
  return {
    range: typeof meta.range === "string" ? (meta.range as TrafficRange) : "day",
    buckets: data?.buckets ?? [],
    chart: data?.chart ?? null,
  };
}
