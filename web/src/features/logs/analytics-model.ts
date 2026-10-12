import type { LogAggregate } from "@/api/types";
import type { LineSeries } from "@/components/charts/multi-line";
import { MAX_SERIES, SERIES_VARS } from "@/components/charts/palette";
import { levelVar } from "@/lib/levels";
import { fmtMs, fmtNumber } from "@/lib/time";
import { fieldLabel } from "@/lib/fields";

export const METRICS = ["count", "avg", "sum", "min", "max", "p50", "p95", "p99"] as const;
export type Metric = (typeof METRICS)[number];

/** Split-by choices offered before the attribute keys discovered from facets. */
export const GROUP_FIELDS: string[] = ["service", "env", "level", "logger", "host", "release", "source", "http.method", "http.route", "http.status", "exception", "user"];

/** Each metric in words; the technical name stays in brackets where people know it. */
export const METRIC_WORDS: Record<Metric, string> = {
  count: "Number of messages",
  avg: "Average",
  sum: "Total",
  min: "Lowest",
  max: "Highest",
  p50: "Typical (p50)",
  p95: "Slow (p95)",
  p99: "Slowest (p99)",
};

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

/** Label for a metric over a field, e.g. "Slow (p95) duration" or "Number of messages". */
export function metricLabel(metric: string, of?: string): string {
  if (metric === "count" || !of) return METRIC_WORDS[metric as Metric] ?? metric;
  return `${METRIC_WORDS[metric as Metric] ?? metric} ${fieldLabel(of).toLowerCase()}`;
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
    out.push({ name: "Other", values: agg.other.series ?? [], color: "var(--lvl-other)" });
  }
  return out;
}

/** Percentage of all rows one group holds. */
export function sharePct(rows: number, total: number): number {
  return total > 0 ? (rows / total) * 100 : 0;
}
