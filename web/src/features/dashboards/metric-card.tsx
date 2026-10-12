import { useMemo } from "react";
import { useMetricNames, useMetricSeries } from "@/api/hooks";
import { ApiRequestError } from "@/api/client";
import { MultiLineChart, type LineSeries } from "@/components/charts/multi-line";
import { SERIES_VARS } from "@/components/charts/palette";
import { Gauge } from "lucide-react";
import { EmptyState, ErrorState, Input, Section, Select } from "@/components/ui";
import { unitFormatter } from "@/lib/time";

export interface MetricCardState {
  name: string;
  agg: string;
  groupBy: string;
}

/**
 * One OpenTelemetry metric over time ("Custom measurements"). The picker lists the project's
 * catalog; the "Show" menu follows the metric type (per second for counters, typical / slow for
 * histograms, average for gauges).
 */
export function MetricCard({ range, scope, state, onState }: { range: { from: string; to: string }; scope: { service?: string; env?: string }; state: MetricCardState; onState: (p: Partial<MetricCardState>) => void }) {
  const names = useMetricNames();
  const items = names.data?.items ?? [];
  const name = state.name || items[0]?.name || "";
  const info = items.find((m) => m.name === name);
  const aggs = info ? (names.data?.aggs[info.type] ?? ["avg"]) : ["avg"];
  const agg = state.agg && aggs.includes(state.agg) ? state.agg : info?.type === "sum" && !info.monotonic ? "avg" : aggs[0];
  const series = useMetricSeries({ name, agg, group_by: state.groupBy || undefined, from: range.from, to: range.to, service: scope.service, env: scope.env, limit: 8, buckets: 60 }, !!name);
  const data = series.data;
  const fmt = useMemo(() => unitFormatter(info?.unit), [info?.unit]);
  const lines = useMemo<LineSeries[]>(() => {
    if (!data) return [];
    const out: LineSeries[] = data.series.map((s, i) => ({ name: s.label || (data.group_by ? "empty" : data.name), values: s.points.map((p) => p.value), color: data.group_by ? SERIES_VARS[i % SERIES_VARS.length] : "var(--accent)" }));
    if (data.other) out.push({ name: "Other", values: data.other.points.map((p) => p.value), color: "var(--lvl-other)" });
    return out;
  }, [data]);
  const err = series.error instanceof ApiRequestError ? series.error.body.message || series.error.message : series.isError ? String(series.error) : null;

  const aggWords: Record<string, string> = { rate: "Per second", avg: "Average", sum: "Total", min: "Lowest", max: "Highest", last: "Latest", count: "Count", p50: "Typical (p50)", p90: "p90", p95: "Slow (p95)", p99: "Slowest (p99)" };
  return (
    <Section
      title="Custom measurements"
      description={info ? `${aggWords[agg] ?? agg} of ${name}${info.unit ? `, in ${unitWords(info.unit)}` : ""}` : "Numbers your apps send as OpenTelemetry metrics."}
      actions={
        items.length > 0 ? (
          <div className="flex flex-wrap items-center justify-end gap-1.5 text-[13px] text-ink-3">
            <Select value={name} onChange={(e) => onState({ name: e.target.value, agg: "" })} className="w-auto min-w-48 max-w-72 font-mono text-[12px]" aria-label="Measurement">
              {items.map((m) => (
                <option key={m.name} value={m.name}>
                  {m.name}
                </option>
              ))}
            </Select>
            <Select value={agg} onChange={(e) => onState({ agg: e.target.value })} className="w-auto" aria-label="Show">
              {aggs.map((a) => (
                <option key={a} value={a}>
                  {aggWords[a] ?? a}
                </option>
              ))}
            </Select>
            <span>split by</span>
            <Input list="metric-group-keys" value={state.groupBy} onChange={(e) => onState({ groupBy: e.target.value })} placeholder="nothing" className="w-36" aria-label="Split by" />
            <datalist id="metric-group-keys">
              <option value="service" />
              <option value="env" />
              <option value="http.route" />
              <option value="http.request.method" />
              <option value="http.response.status_code" />
              <option value="host.name" />
            </datalist>
          </div>
        ) : undefined
      }
    >
      {names.isError && !names.data ? (
        <ErrorState compact error={names.error} retry={() => names.refetch()} title="Couldn't load the measurements" />
      ) : items.length === 0 && !names.isPending ? (
        <EmptyState
          icon={<Gauge size={18} />}
          title="No custom measurements yet"
          hint="Apps that send OpenTelemetry metrics show up here, for example request durations from FastAPI's built-in telemetry. See Projects for how to connect an app."
          className="py-8"
        />
      ) : err ? (
        <div role="alert" className="px-2 py-6 text-center text-[13px] text-critical-text">
          {err}
        </div>
      ) : (
        <MultiLineChart buckets={data?.series[0]?.points.map((p) => p.ts) ?? data?.other?.points.map((p) => p.ts) ?? []} series={lines} bucketSeconds={data?.bucket_seconds} height={220} format={fmt} loading={series.isPending || names.isPending} emptyText="Nothing measured in this time range" />
      )}
    </Section>
  );
}

/** OpenTelemetry unit codes in words. */
function unitWords(unit: string): string {
  const w: Record<string, string> = { s: "seconds", ms: "milliseconds", us: "microseconds", ns: "nanoseconds", By: "bytes", "1": "plain numbers", "%": "percent", "{request}": "requests" };
  return w[unit] ?? unit.replace(/[{}]/g, "");
}
