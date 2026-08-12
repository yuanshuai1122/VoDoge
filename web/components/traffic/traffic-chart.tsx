"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorState } from "@/components/common/empty-state";
import {
  getTrafficAnalysis,
  type TrafficRange,
} from "@/lib/api/endpoints/traffic";
import { formatBytes } from "@/lib/format";

/**
 * 分类色槽，固定顺序取用、**绝不循环**：颜色跟随设备身份，
 * 过滤掉某些设备时其余设备不能改色。色板已通过色觉安全校验（见 globals.css）。
 */
const SERIES_COLORS = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
];

/** 超出色槽数量的设备折叠为「其它」，而不是生成新颜色。 */
const OTHER_KEY = "__other__";
const OTHER_COLOR = "var(--muted-foreground)";

const RANGES: { value: TrafficRange; label: string }[] = [
  { value: "day", label: "日" },
  { value: "week", label: "周" },
  { value: "month", label: "月" },
];

interface SeriesMeta {
  key: string;
  label: string;
  color: string;
  total: number;
}

export function TrafficChart({ deviceId }: { deviceId?: string }) {
  const [range, setRange] = useState<TrafficRange>("day");

  const query = useQuery({
    queryKey: ["traffic", range, deviceId ?? "all"],
    queryFn: () => getTrafficAnalysis({ range, deviceId }),
  });

  const { rows, series } = useMemo(() => {
    const chart = query.data?.chart;
    if (!chart?.timestamps?.length) {
      return { rows: [], series: [] as SeriesMeta[] };
    }

    const devices = chart.devices ?? [];

    // 按总量排序后取色：颜色分配稳定地跟随设备，且重要序列优先拿到高辨识度色槽
    const totals = devices.map((id) => ({
      id,
      total: (chart.series?.[id] ?? []).reduce((a, b) => a + (b || 0), 0),
    }));
    totals.sort((a, b) => b.total - a.total);

    const top = totals.slice(0, SERIES_COLORS.length);
    const rest = totals.slice(SERIES_COLORS.length);

    const meta: SeriesMeta[] = top.map((t, i) => ({
      key: t.id,
      label: t.id,
      color: SERIES_COLORS[i],
      total: t.total,
    }));

    if (rest.length > 0) {
      meta.push({
        key: OTHER_KEY,
        label: `其它 ${rest.length} 台`,
        color: OTHER_COLOR,
        total: rest.reduce((a, b) => a + b.total, 0),
      });
    }

    const rows = chart.timestamps.map((ts, i) => {
      const row: Record<string, string | number> = { time: ts };
      for (const t of top) {
        row[t.id] = chart.series?.[t.id]?.[i] ?? 0;
      }
      if (rest.length > 0) {
        row[OTHER_KEY] = rest.reduce(
          (sum, t) => sum + (chart.series?.[t.id]?.[i] ?? 0),
          0,
        );
      }
      return row;
    });

    return { rows, series: meta };
  }, [query.data]);

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-4">
        <CardTitle className="text-base">流量趋势</CardTitle>
        <div className="flex gap-1">
          {RANGES.map((r) => (
            <Button
              key={r.value}
              variant={range === r.value ? "secondary" : "ghost"}
              size="xs"
              onClick={() => setRange(r.value)}
            >
              {r.label}
            </Button>
          ))}
        </div>
      </CardHeader>

      <CardContent>
        {query.isError ? (
          <ErrorState error={query.error} onRetry={() => query.refetch()} />
        ) : query.isPending ? (
          <Skeleton className="h-64" />
        ) : rows.length === 0 ? (
          <EmptyState
            title="暂无流量数据"
            description="设备产生流量后，这里会按所选周期汇总展示。"
          />
        ) : (
          <>
            <div className="h-64 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart
                  data={rows}
                  margin={{ top: 8, right: 8, bottom: 0, left: 8 }}
                >
                  {/* 网格与坐标轴保持弱化，不与数据线争视觉 */}
                  <CartesianGrid
                    stroke="var(--border)"
                    strokeDasharray="3 3"
                    vertical={false}
                  />
                  <XAxis
                    dataKey="time"
                    tick={{ fontSize: 11, fill: "var(--muted-foreground)" }}
                    tickLine={false}
                    axisLine={{ stroke: "var(--border)" }}
                    minTickGap={24}
                  />
                  {/* 单一数值轴：不同量级的指标应拆图，绝不用双轴 */}
                  <YAxis
                    tick={{ fontSize: 11, fill: "var(--muted-foreground)" }}
                    tickLine={false}
                    axisLine={false}
                    width={64}
                    tickFormatter={(v: number) => formatBytes(v)}
                  />
                  <Tooltip
                    cursor={{ stroke: "var(--muted-foreground)", strokeWidth: 1 }}
                    content={<TrafficTooltip series={series} />}
                  />
                  {series.map((s) => (
                    <Line
                      key={s.key}
                      type="monotone"
                      dataKey={s.key}
                      name={s.label}
                      stroke={s.color}
                      strokeWidth={2}
                      dot={false}
                      activeDot={{ r: 4, strokeWidth: 0 }}
                      isAnimationActive={false}
                    />
                  ))}
                </LineChart>
              </ResponsiveContainer>
            </div>

            {/* 多序列时图例始终存在，身份不靠颜色单独承载 */}
            <Legend series={series} />
          </>
        )}
      </CardContent>
    </Card>
  );
}

function Legend({ series }: { series: SeriesMeta[] }) {
  return (
    <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1.5">
      {series.map((s) => (
        <li key={s.key} className="flex items-center gap-1.5 text-xs">
          <span
            aria-hidden
            className="size-2.5 shrink-0 rounded-full"
            style={{ background: s.color }}
          />
          {/* 文字用文本色，不涂序列色 */}
          <span className="text-foreground">{s.label}</span>
          <span className="tabular-nums text-muted-foreground">
            {formatBytes(s.total)}
          </span>
        </li>
      ))}
    </ul>
  );
}

interface TooltipPayloadItem {
  dataKey?: string | number;
  value?: number;
}

function TrafficTooltip({
  active,
  payload,
  label,
  series,
}: {
  active?: boolean;
  payload?: TooltipPayloadItem[];
  label?: string;
  series: SeriesMeta[];
}) {
  if (!active || !payload?.length) return null;

  const byKey = new Map(series.map((s) => [s.key, s]));

  return (
    <div className="rounded-lg border bg-popover p-2.5 text-popover-foreground shadow-md">
      <p className="mb-1.5 text-xs font-medium">{label}</p>
      <ul className="flex flex-col gap-1">
        {payload.map((item) => {
          const meta = byKey.get(String(item.dataKey));
          if (!meta) return null;
          return (
            <li
              key={meta.key}
              className="flex items-center justify-between gap-4 text-xs"
            >
              <span className="flex items-center gap-1.5">
                <span
                  aria-hidden
                  className="size-2 shrink-0 rounded-full"
                  style={{ background: meta.color }}
                />
                <span>{meta.label}</span>
              </span>
              <span className="tabular-nums">{formatBytes(item.value ?? 0)}</span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
