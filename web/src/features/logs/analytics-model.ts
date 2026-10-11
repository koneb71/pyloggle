import type { LogAggregate } from "@/api/types";
import type { LineSeries } from "@/components/charts/multi-line";
import { MAX_SERIES, SERIES_VARS } from "@/components/charts/palette";
import { levelVar } from "@/lib/levels";
import { fmtMs, fmtNumber } from "@/lib/time";

export const METRICS = ["count", "avg", "sum", "min", "max", "p50", "p95", "p99"] as const;
export type Metric = (typeof METRICS)[number];

/** Group-by choices offered before the attribute keys discovered from facets. */
export const GROUP_FIELDS: { field: string; label: string }[] = [
  { field: "service", label: "service" },
  { field: "env", label: "env" },
  { field: "level", label: "level" },
  { field: "logger", label: "logger" },
  { field: "host", label: "host" },
  { field: "release", label: "release" },
  { field: "source", label: "source" },
  { field: "http.method", label: "http.method" },
  { field: "http.route", label: "http.route" },
  { field: "http.status", label: "http.status" },
  { field: "exception", label: "exception" },
  { field: "user", label: "user" },
];

/** Numeric fields the non-count metrics can measure (attribute keys are added at runtime). */
export const OF_FIELDS = ["duration_ms", "http.status"];

export interface AnalyticsState {
  groupBy: string;
  metric: Metric;
  of: string;
  limit: number;
}

export const DEFAULT_ANALYTICS: AnalyticsState = { groupBy: "service", metric: "count", of: "duration_ms", limit: 8 };

export function isMetric(s: string): s is Metric {
  return (METRICS as readonly string[]).includes(s);
}

/** Label for a metric over a field, e.g. "p95 duration_ms" or "count". */
export function metricLabel(metric: string, of?: string): string {
  return metric === "count" ? "count" : `${metric} ${of ?? ""}`.trim();
}

/** Value formatter for a metric: latencies as ms, everything else as plain numbers. */
export function metricFormat(metric: string, of?: string): (v: number) => string {
  if (metric !== "count" && (of === "duration_ms" || of === "duration" || of === "latency_ms")) return fmtMs;
  if (metric === "count" || metric === "sum") return fmtNumber;
  return (v: number) => (Number.isInteger(v) ? v.toLocaleString() : v.toFixed(2));
}

/**
 * Series for the line chart: the top groups in server order, at most MAX_SERIES, plus the
 * server's "other" fold only when it is exact (every ranked group is on the chart). Level
 * groups keep their level colors.
 */
export function chartSeries(agg: LogAggregate | undefined): LineSeries[] {
  if (!agg) return [];
  if (!agg.group_by) return [{ name: metricLabel(agg.metric, agg.of), values: agg.total.series ?? [], color: "var(--accent)" }];
  const isLevel = agg.group_by === "level";
  const out: LineSeries[] = agg.groups.slice(0, MAX_SERIES).map((g, i) => ({ name: g.key, values: g.series ?? [], color: isLevel ? levelVar(g.key) : SERIES_VARS[i % SERIES_VARS.length] }));
  // "other" is the server's exact remainder of the ranked groups; it only fits when every
  // ranked group is on the chart and a slot is left for it.
  if (agg.other && agg.groups.length < MAX_SERIES) {
    out.push({ name: "other", values: agg.other.series ?? [], color: "var(--lvl-other)" });
  }
  return out;
}

/** Percentage of all rows one group holds. */
export function sharePct(rows: number, total: number): number {
  return total > 0 ? (rows / total) * 100 : 0;
}
