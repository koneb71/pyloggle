import type { ChartSource, LogAggregate } from "@/api/types";
import type { LineSeries } from "@/components/charts/multi-line";
import { MAX_SERIES, SERIES_VARS } from "@/components/charts/palette";
import { levelVar } from "@/lib/levels";
import { fmtMs, fmtNumber, fmtPct } from "@/lib/time";
import { fieldLabel } from "@/lib/fields";

export type Metric = "count" | "people" | "problems" | "failed" | "failure_rate" | "avg" | "sum" | "min" | "max" | "p50" | "p95" | "p99";

export const SOURCES: ChartSource[] = ["messages", "errors", "requests"];

/** What a chart counts, in words. */
export const SOURCE_WORDS: Record<ChartSource, { label: string; one: string; many: string; hint: string }> = {
  messages: { label: "Messages", one: "message", many: "messages", hint: "Everything your apps wrote" },
  errors: { label: "Errors", one: "error", many: "errors", hint: "Each error your apps reported" },
  requests: { label: "Requests", one: "request", many: "requests", hint: "Every web request your apps answered, counted" },
};

/** Each source's metrics, in menu order (the server checks the same lists). */
export const METRICS: Record<ChartSource, Metric[]> = {
  messages: ["count", "people", "avg", "sum", "min", "max", "p50", "p95", "p99"],
  errors: ["count", "problems", "people", "avg", "sum", "min", "max", "p50", "p95", "p99"],
  requests: ["count", "failed", "failure_rate", "people", "p50", "p95", "p99"],
};

/** Split-by choices offered before the attribute keys discovered from facets. */
export const GROUP_FIELDS: Record<ChartSource, string[]> = {
  messages: ["service", "env", "level", "logger", "host", "release", "source", "http.method", "http.route", "http.status", "exception", "user"],
  errors: ["exception", "service", "env", "release", "http.route", "http.method", "http.status", "host", "logger", "handled", "user"],
  requests: ["endpoint", "http.route", "http.method", "service", "env", "release"],
};

/** Numeric fields the measuring metrics can measure (attribute keys are added at runtime). */
export const OF_FIELDS: Record<ChartSource, string[]> = {
  messages: ["duration_ms", "http.status"],
  errors: ["http.status"],
  requests: [],
};

export interface ChartState {
  source: ChartSource;
  q: string;
  groupBy: string;
  metric: Metric;
  of: string;
  limit: number;
}

/** A fresh chart of each source: how many, split by what tells them apart best. */
export const DEFAULT_CHART: Record<ChartSource, ChartState> = {
  messages: { source: "messages", q: "", groupBy: "service", metric: "count", of: "duration_ms", limit: 8 },
  errors: { source: "errors", q: "", groupBy: "exception", metric: "count", of: "http.status", limit: 8 },
  requests: { source: "requests", q: "", groupBy: "endpoint", metric: "count", of: "", limit: 8 },
};

export function isSource(s: string): s is ChartSource {
  return (SOURCES as string[]).includes(s);
}

export function isMetric(source: ChartSource, s: string): s is Metric {
  return (METRICS[source] as string[]).includes(s);
}

/** Whether a metric measures a field: every metric but the counts, and none over requests. */
export function needsField(source: ChartSource, metric: string): boolean {
  if (source === "requests") return false;
  return !["count", "people", "problems"].includes(metric);
}

/** Each metric in words; the technical name stays in brackets where people know it. */
export function metricWord(source: ChartSource, metric: string): string {
  const response = source === "requests";
  switch (metric) {
    case "count":
      return `Number of ${SOURCE_WORDS[source].many}`;
    case "people":
      return "People";
    case "problems":
      return "Problems";
    case "failed":
      return "Failed requests";
    case "failure_rate":
      return "Share that failed";
    case "avg":
      return "Average";
    case "sum":
      return "Total";
    case "min":
      return "Lowest";
    case "max":
      return "Highest";
    case "p50":
      return response ? "Typical response time (p50)" : "Typical (p50)";
    case "p95":
      return response ? "Slow response time (p95)" : "Slow (p95)";
    case "p99":
      return response ? "Slowest response time (p99)" : "Slowest (p99)";
  }
  return metric;
}

/** Label for a metric over a field, e.g. "Slow (p95) duration" or "Number of errors". */
export function metricLabel(source: ChartSource, metric: string, of?: string): string {
  const word = metricWord(source, metric);
  if (!needsField(source, metric) || !of) return word;
  return `${word} ${fieldLabel(of).toLowerCase()}`;
}

/** Value formatter for a metric: times as ms, shares as percentages, counts as numbers. */
export function metricFormat(source: ChartSource, metric: string, of?: string): (v: number) => string {
  if (source === "requests" && metric.startsWith("p") && metric !== "people") return fmtMs;
  if (metric === "failure_rate") return fmtPct;
  if (needsField(source, metric) && (of === "duration_ms" || of === "duration" || of === "latency_ms")) return fmtMs;
  if (!needsField(source, metric) || metric === "sum") return fmtNumber;
  return (v: number) => (Number.isInteger(v) ? v.toLocaleString() : v.toFixed(2));
}

/** Counts that stack: drawn as bars, split into colors. Everything else is a line per group. */
export function stacks(metric: string): boolean {
  return metric === "count";
}

/**
 * Series for the line chart: the top groups in server order, at most MAX_SERIES, plus the
 * server's "other" fold only when it is exact (every ranked group is on the chart). Level
 * groups keep their level colors.
 */
export function chartSeries(agg: LogAggregate | undefined, source: ChartSource = "messages"): LineSeries[] {
  if (!agg) return [];
  if (!agg.group_by) return [{ name: metricLabel(source, agg.metric, agg.of), values: agg.total.series ?? [], color: "var(--accent)" }];
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

/** A chart as URL parameters (defaults left out), so it can be linked, saved and pinned. */
export function chartParams(s: ChartState): Record<string, string | undefined> {
  const d = DEFAULT_CHART[s.source];
  return {
    source: s.source === "messages" ? undefined : s.source,
    q: s.q || undefined,
    metric: s.metric === d.metric ? undefined : s.metric,
    of: needsField(s.source, s.metric) && s.of !== d.of ? s.of : undefined,
    group_by: s.groupBy === d.groupBy ? undefined : s.groupBy || "none",
    top: s.limit === d.limit ? undefined : String(s.limit),
  };
}

/** The chart a set of URL parameters describes; unknown values fall back to the source's defaults. */
export function chartFromParams(get: (key: string) => string): ChartState {
  const raw = get("source");
  const source: ChartSource = isSource(raw) ? raw : "messages";
  const d = DEFAULT_CHART[source];
  const m = get("metric");
  const gb = get("group_by");
  return {
    source,
    q: get("q"),
    metric: isMetric(source, m) ? m : d.metric,
    of: get("of") || d.of,
    groupBy: gb === "none" ? "" : gb || d.groupBy,
    limit: Number(get("top")) || d.limit,
  };
}

/** A chart saved with "Save chart": the JSON kept in a saved view. */
export interface SavedChart {
  kind: "chart";
  source: ChartSource;
  q: string;
  metric: Metric;
  of?: string;
  group_by: string;
  limit: number;
  from: string;
  to?: string;
}

export function savedChart(s: ChartState, range: { from: string; to: string }): SavedChart {
  return { kind: "chart", source: s.source, q: s.q, metric: s.metric, of: needsField(s.source, s.metric) ? s.of : undefined, group_by: s.groupBy, limit: s.limit, from: range.from, to: range.to || undefined };
}

/** The chart in a saved view, or null when the view is a Logs search. */
export function parseSavedChart(query: string): SavedChart | null {
  try {
    const o = JSON.parse(query) as Partial<SavedChart>;
    if (o?.kind !== "chart" || !isSource(o.source ?? "")) return null;
    const source = o.source as ChartSource;
    const d = DEFAULT_CHART[source];
    return {
      kind: "chart",
      source,
      q: o.q ?? "",
      metric: isMetric(source, o.metric ?? "") ? (o.metric as Metric) : d.metric,
      of: o.of,
      group_by: o.group_by ?? d.groupBy,
      limit: o.limit || d.limit,
      from: o.from || "-24h",
      to: o.to,
    };
  } catch {
    return null;
  }
}

export function chartStateOf(c: SavedChart): ChartState {
  return { source: c.source, q: c.q, metric: c.metric, of: c.of ?? DEFAULT_CHART[c.source].of, groupBy: c.group_by, limit: c.limit };
}

/** Where a chart opens in Analytics. */
export function chartHref(s: ChartState, range?: { from: string; to?: string }, scope?: { service?: string; env?: string }): string {
  const p = new URLSearchParams({ tab: "charts" });
  for (const [k, v] of Object.entries({ ...chartParams(s), from: range?.from, to: range?.to || undefined, service: scope?.service, env: scope?.env })) {
    if (v) p.set(k, v);
  }
  return `/analytics?${p.toString()}`;
}

/**
 * The Analytics chart an old Logs Charts link meant: /logs?view=analytics&metric=p95&
 * group_by_a=http.route&of=...&top=...&q=...&from=... (Dashboards, bookmarks, saved searches).
 */
export function chartHrefFromLogs(params: URLSearchParams): string {
  const get = (k: string) => params.get(k) ?? "";
  const s: ChartState = chartFromParams((k) => (k === "group_by" ? get("group_by_a") : k === "source" ? "messages" : get(k)));
  return chartHref(s, get("from") ? { from: get("from"), to: get("to") } : undefined);
}
