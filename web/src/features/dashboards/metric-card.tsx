import { useMemo } from "react";
import { useMetricNames, useMetricSeries } from "@/api/hooks";
import { ApiRequestError } from "@/api/client";
import { MultiLineChart, type LineSeries } from "@/components/charts/multi-line";
import { SERIES_VARS } from "@/components/charts/palette";
import { Card, CardHeader, EmptyState, Input, Select } from "@/components/ui";
import { bucketLabel, unitFormatter } from "@/lib/time";

export interface MetricCardState {
  name: string;
  agg: string;
  groupBy: string;
}

/**
 * One OTLP metric over time. The picker lists the project's catalog; the aggregation menu
 * follows the metric type (rate for counters, quantiles for histograms, avg for gauges).
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
    const out: LineSeries[] = data.series.map((s, i) => ({ name: s.label || (data.group_by ? "(empty)" : `${data.agg} ${data.name}`), values: s.points.map((p) => p.value), color: data.group_by ? SERIES_VARS[i % SERIES_VARS.length] : "var(--accent)" }));
    if (data.other) out.push({ name: "other", values: data.other.points.map((p) => p.value), color: "var(--lvl-other)" });
    return out;
  }, [data]);
  const err = series.error instanceof ApiRequestError ? series.error.body.message || series.error.message : series.isError ? String(series.error) : null;

  return (
    <Card>
      <CardHeader
        title="Metrics"
        sub={info ? `${info.type}${info.unit ? ` · ${info.unit}` : ""}${data ? ` · ${bucketLabel(data.bucket_seconds)} buckets` : ""}` : "OpenTelemetry metrics received on /v1/metrics"}
        actions={
          items.length > 0 ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <Select value={name} onChange={(e) => onState({ name: e.target.value, agg: "" })} className="h-7 w-56 py-0 text-xs" aria-label="Metric">
                {items.map((m) => (
                  <option key={m.name} value={m.name}>
                    {m.name}
                  </option>
                ))}
              </Select>
              <Select value={agg} onChange={(e) => onState({ agg: e.target.value })} className="h-7 w-20 py-0 text-xs" aria-label="Aggregation">
                {aggs.map((a) => (
                  <option key={a} value={a}>
                    {a}
                  </option>
                ))}
              </Select>
              <span className="text-[11px] text-ink-3">by</span>
              <Input list="metric-group-keys" value={state.groupBy} onChange={(e) => onState({ groupBy: e.target.value })} placeholder="attribute" className="h-7 w-32 text-xs" aria-label="Group by attribute" />
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
      />
      <div className="p-2">
        {items.length === 0 && !names.isPending ? (
          <EmptyState title="No OTLP metrics yet" hint="Point OTEL_EXPORTER_OTLP_ENDPOINT at this server with your project's API key; FastAPI 0.142+ native telemetry and the contrib instrumentations send HTTP metrics automatically." className="py-8" />
        ) : err ? (
          <div className="px-2 py-6 text-center text-xs text-critical">{err}</div>
        ) : (
          <MultiLineChart buckets={data?.series[0]?.points.map((p) => p.ts) ?? data?.other?.points.map((p) => p.ts) ?? []} series={lines} bucketSeconds={data?.bucket_seconds} height={200} format={fmt} loading={series.isPending || names.isPending} emptyText="No samples in this range" />
        )}
      </div>
    </Card>
  );
}
