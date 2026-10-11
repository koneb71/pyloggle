import { useMemo } from "react";
import clsx from "clsx";
import { useLogAggregate, useLogFacets } from "@/api/hooks";
import { ApiRequestError } from "@/api/client";
import { MultiLineChart } from "@/components/charts/multi-line";
import { TimelineHistogram } from "@/components/charts/timeline-histogram";
import { EmptyState, ErrorState, Select, Skeleton } from "@/components/ui";
import { fmtNumber } from "@/lib/time";
import { levelVar } from "@/lib/levels";
import { SERIES_VARS } from "@/components/charts/palette";
import { chartSeries, DEFAULT_ANALYTICS, GROUP_FIELDS, METRICS, metricFormat, metricLabel, OF_FIELDS, sharePct, type AnalyticsState } from "./analytics-model";

export interface AnalyticsViewProps {
  q: string;
  range: { from: string; to: string };
  state: AnalyticsState;
  onState: (patch: Partial<AnalyticsState>) => void;
  onFilter: (field: string, value: string) => void;
  onZoom?: (from: Date, to: Date) => void;
}

const LIMITS = [5, 8, 15, 25];

/** Analytics mode of the explorer: a metric over a field, split by a group, over time. */
export function AnalyticsView({ q, range, state, onState, onFilter, onZoom }: AnalyticsViewProps) {
  const facets = useLogFacets({ q, from: range.from, to: range.to, fields: ["service"], attr_keys: 40 });
  const attrKeys = useMemo(() => (facets.data?.attr_keys ?? []).map((k) => `attrs.${k.value}`), [facets.data]);
  const agg = useLogAggregate({ q, from: range.from, to: range.to, group_by: state.groupBy, metric: state.metric, of: state.metric === "count" ? undefined : state.of, limit: state.limit, buckets: 60 });
  const data = agg.data;
  const series = useMemo(() => chartSeries(data), [data]);
  const fmt = metricFormat(state.metric, state.of);
  const label = metricLabel(state.metric, state.metric === "count" ? undefined : state.of);
  const err = agg.error instanceof ApiRequestError && agg.error.status === 400 ? agg.error.body.message || agg.error.message : null;
  const total = data?.total.rows ?? 0;
  const rows = useMemo(() => {
    if (!data) return [];
    const list = data.groups.map((g, i) => ({ ...g, color: data.group_by === "level" ? levelVar(g.key) : SERIES_VARS[i % SERIES_VARS.length], other: false }));
    if (data.other) list.push({ ...data.other, color: "var(--lvl-other)", other: true });
    return list;
  }, [data]);
  const groupedChartLimited = data && data.groups.length > 8;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-none flex-wrap items-center gap-2 border-b border-border bg-surface px-3 py-2 text-xs">
        <span className="text-ink-3">Group by</span>
        <Select value={state.groupBy} onChange={(e) => onState({ groupBy: e.target.value })} className="h-7 w-44 py-0 text-xs" aria-label="Group by">
          <option value="">— none —</option>
          {GROUP_FIELDS.map((f) => (
            <option key={f.field} value={f.field}>
              {f.label}
            </option>
          ))}
          {attrKeys.length > 0 && (
            <optgroup label="Attributes">
              {attrKeys.map((k) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </optgroup>
          )}
          {state.groupBy && !GROUP_FIELDS.some((f) => f.field === state.groupBy) && !attrKeys.includes(state.groupBy) && <option value={state.groupBy}>{state.groupBy}</option>}
        </Select>
        <span className="text-ink-3">Metric</span>
        <Select value={state.metric} onChange={(e) => onState({ metric: e.target.value as AnalyticsState["metric"] })} className="h-7 w-24 py-0 text-xs" aria-label="Metric">
          {METRICS.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </Select>
        {state.metric !== "count" && (
          <>
            <span className="text-ink-3">of</span>
            <Select value={state.of} onChange={(e) => onState({ of: e.target.value })} className="h-7 w-44 py-0 text-xs" aria-label="Measured field">
              {OF_FIELDS.map((f) => (
                <option key={f} value={f}>
                  {f}
                </option>
              ))}
              {attrKeys.length > 0 && (
                <optgroup label="Attributes (numeric)">
                  {attrKeys.map((k) => (
                    <option key={k} value={k}>
                      {k}
                    </option>
                  ))}
                </optgroup>
              )}
              {state.of && !OF_FIELDS.includes(state.of) && !attrKeys.includes(state.of) && <option value={state.of}>{state.of}</option>}
            </Select>
          </>
        )}
        {state.groupBy && (
          <>
            <span className="text-ink-3">Top</span>
            <Select value={state.limit} onChange={(e) => onState({ limit: Number(e.target.value) })} className="h-7 w-16 py-0 text-xs" aria-label="Number of groups">
              {LIMITS.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </Select>
          </>
        )}
        {agg.isFetching && <span className="text-ink-3">updating…</span>}
        <button type="button" className="ml-auto text-ink-3 hover:text-ink" onClick={() => onState(DEFAULT_ANALYTICS)}>
          reset
        </button>
      </div>
      {err ? (
        <div className="m-3 rounded-md border border-critical/40 px-3 py-2 text-xs text-critical">{err}</div>
      ) : agg.isError ? (
        <ErrorState error={agg.error} retry={() => agg.refetch()} />
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="border-b border-border px-3 pt-2">
            <div className="flex items-baseline gap-3 text-xs text-ink-3">
              <span className="text-[13px] font-semibold text-ink">{label}</span>
              {data && (
                <span>
                  {fmtNumber(total)} rows
                  {data.total.value !== null && state.metric !== "count" && <> · overall {fmt(data.total.value)}</>}
                  {groupedChartLimited && <> · chart shows the top 8 of {data.groups.length}</>}
                </span>
              )}
            </div>
            {state.metric === "count" ? (
              <TimelineHistogram data={data?.histogram} loading={agg.isPending} height={200} onZoom={onZoom} className="mt-1" />
            ) : (
              <MultiLineChart buckets={data?.buckets ?? []} series={series} bucketSeconds={data?.bucket_seconds} height={200} format={fmt} loading={agg.isPending} className="mt-1" />
            )}
          </div>
          {agg.isPending ? (
            <Skeleton rows={6} className="p-4" />
          ) : !data || total === 0 ? (
            <EmptyState title="No rows match" hint="Widen the time range or remove a filter." className="py-8" />
          ) : (
            <table className="w-full text-[13px]">
              <thead className="text-left text-[11px] uppercase tracking-wide text-ink-3">
                <tr className="border-b border-border">
                  <th className="px-3 py-1.5 font-medium">{data.group_by || "all rows"}</th>
                  <th className="px-3 py-1.5 text-right font-medium">rows</th>
                  <th className="w-40 px-3 py-1.5 font-medium">share</th>
                  <th className="px-3 py-1.5 text-right font-medium">{label}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((g) => (
                  <tr key={g.other ? "__other" : g.key} className="border-b border-border hover:bg-surface-2">
                    <td className="max-w-0 px-3 py-1.5">
                      <div className="flex items-center gap-2">
                        <span className="inline-block h-2.5 w-2.5 flex-none rounded-sm" style={{ background: g.color }} aria-hidden />
                        {g.other ? (
                          <span className="text-ink-2">other</span>
                        ) : data.group_by ? (
                          <button type="button" className="truncate font-mono text-ink hover:text-accent" title={`Filter ${data.group_by}:${g.key}`} onClick={() => onFilter(data.group_by, g.key)}>
                            {g.key === "" ? <em className="font-sans text-ink-3">(empty)</em> : g.key}
                          </button>
                        ) : (
                          <span className="text-ink">all rows</span>
                        )}
                      </div>
                    </td>
                    <td className="px-3 py-1.5 text-right tabular text-ink-2">{fmtNumber(g.rows)}</td>
                    <td className="px-3 py-1.5">
                      <div className="flex items-center gap-2">
                        <span className="relative flex h-1.5 flex-1 rounded bg-surface-3">
                          <span className="h-1.5 rounded" style={{ width: `${Math.max(1, sharePct(g.rows, total))}%`, background: g.color }} />
                        </span>
                        <span className="w-12 text-right tabular text-[11px] text-ink-3">{sharePct(g.rows, total).toFixed(1)}%</span>
                      </div>
                    </td>
                    <td className={clsx("px-3 py-1.5 text-right font-medium tabular", g.value === null ? "text-ink-3" : "text-ink")}>{g.value === null ? "–" : fmt(g.value)}</td>
                  </tr>
                ))}
                {data.group_by && (
                  <tr className="bg-surface-2/60 font-medium">
                    <td className="px-3 py-1.5 text-ink-2">total</td>
                    <td className="px-3 py-1.5 text-right tabular text-ink">{fmtNumber(total)}</td>
                    <td className="px-3 py-1.5 text-right text-[11px] text-ink-3">100%</td>
                    <td className="px-3 py-1.5 text-right tabular text-ink">{data.total.value === null ? "–" : fmt(data.total.value)}</td>
                  </tr>
                )}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  );
}
