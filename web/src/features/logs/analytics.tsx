import { useMemo } from "react";
import clsx from "clsx";
import { useLogAggregate, useLogFacets } from "@/api/hooks";
import type { AggregateGroup } from "@/api/types";
import { ApiRequestError } from "@/api/client";
import { MultiLineChart } from "@/components/charts/multi-line";
import { TimelineHistogram } from "@/components/charts/timeline-histogram";
import { DataTable, EmptyState, ErrorState, Select, Swatch, UpdatingDot, type Column } from "@/components/ui";
import { fmtNumber } from "@/lib/time";
import { levelLabel, levelVar } from "@/lib/levels";
import { fieldLabel, isCodeLike } from "@/lib/fields";
import { SERIES_VARS } from "@/components/charts/palette";
import { chartSeries, DEFAULT_ANALYTICS, GROUP_FIELDS, METRICS, METRIC_WORDS, metricFormat, metricLabel, OF_FIELDS, sharePct, type AnalyticsState } from "./analytics-model";

export interface AnalyticsViewProps {
  q: string;
  range: { from: string; to: string };
  state: AnalyticsState;
  onState: (patch: Partial<AnalyticsState>) => void;
  onFilter: (field: string, value: string) => void;
  onZoom?: (from: Date, to: Date) => void;
}

const LIMITS = [5, 8, 15, 25];

type Row = AggregateGroup & { color: string; other: boolean; total?: boolean };

/** Charts: count or measure messages over time, split by a field. */
export function AnalyticsView({ q, range, state, onState, onFilter, onZoom }: AnalyticsViewProps) {
  const facets = useLogFacets({ q, from: range.from, to: range.to, fields: ["service"], attr_keys: 40 });
  const attrKeys = useMemo(() => (facets.data?.attr_keys ?? []).map((k) => `attrs.${k.value}`), [facets.data]);
  const agg = useLogAggregate({ q, from: range.from, to: range.to, group_by: state.groupBy, metric: state.metric, of: state.metric === "count" ? undefined : state.of, limit: state.limit, buckets: 60 });
  const data = agg.data;
  const series = useMemo(() => chartSeries(data), [data]);
  const fmt = metricFormat(state.metric, state.of);
  const label = metricLabel(state.metric, state.metric === "count" ? undefined : state.of);
  const badQuery = agg.error instanceof ApiRequestError && agg.error.status === 400 ? agg.error.body.message || agg.error.message : null;
  const total = data?.total.rows ?? 0;
  const groupLabel = data?.group_by ? fieldLabel(data.group_by) : "";
  const rows = useMemo<Row[]>(() => {
    if (!data) return [];
    const list: Row[] = data.groups.map((g, i) => ({ ...g, color: data.group_by === "level" ? levelVar(g.key) : SERIES_VARS[i % SERIES_VARS.length], other: false }));
    if (data.other) list.push({ ...data.other, color: "var(--lvl-other)", other: true });
    if (data.group_by) list.push({ ...data.total, key: "__total", color: "", other: false, total: true });
    return list;
  }, [data]);
  const groupedChartLimited = data && data.groups.length > 8;

  const columns: Column<Row>[] = [
    {
      id: "group",
      header: groupLabel || "All messages",
      cell: (g) =>
        g.total ? (
          <span className="font-medium text-ink-2">All</span>
        ) : (
          <span className="flex min-w-0 items-center gap-2">
            <Swatch color={g.color} />
            {g.other ? (
              <span className="text-ink-2">Other</span>
            ) : data?.group_by ? (
              <button type="button" className={clsx("truncate text-ink hover:text-accent", isCodeLike(data.group_by, g.key) && "font-mono text-[12px]")} title={`Show only ${groupLabel}: ${g.key}`} onClick={() => onFilter(data.group_by, g.key)}>
                {g.key === "" ? <em className="text-ink-3">empty</em> : data.group_by === "level" ? levelLabel(g.key) : g.key}
              </button>
            ) : (
              <span className="text-ink">All messages</span>
            )}
          </span>
        ),
    },
    { id: "rows", header: "Messages", width: "110px", align: "right", hideBelow: state.metric === "count" ? undefined : "sm", cell: (g) => <span className={g.total ? "font-medium text-ink" : "text-ink-2"}>{fmtNumber(g.rows)}</span> },
    {
      id: "share",
      header: "Share",
      width: "180px",
      hideBelow: "lg",
      cell: (g) =>
        g.total ? (
          <span className="w-full text-right text-xs text-ink-3">100%</span>
        ) : (
          <span className="flex w-full items-center gap-2">
            <span className="relative flex h-1.5 flex-1 rounded-full bg-surface-3">
              <span className="h-1.5 rounded-full" style={{ width: `${Math.max(1, sharePct(g.rows, total))}%`, background: g.color }} />
            </span>
            <span className="w-12 text-right text-xs tabular text-ink-3">{sharePct(g.rows, total).toFixed(1)}%</span>
          </span>
        ),
    },
    ...(state.metric === "count"
      ? []
      : [{ id: "value", header: label, width: "150px", align: "right" as const, cell: (g: Row) => <span className={clsx("font-medium", g.value === null ? "text-ink-3" : "text-ink")}>{g.value === null ? "—" : fmt(g.value)}</span> }]),
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-none flex-wrap items-center gap-x-2 gap-y-2 border-b border-border bg-surface px-4 py-2.5 text-[13px]">
        <span className="text-ink-3">Show</span>
        <Select value={state.metric} onChange={(e) => onState({ metric: e.target.value as AnalyticsState["metric"] })} className="w-auto min-w-40" aria-label="What to show">
          {METRICS.map((m) => (
            <option key={m} value={m}>
              {METRIC_WORDS[m]}
            </option>
          ))}
        </Select>
        {state.metric !== "count" && (
          <>
            <span className="text-ink-3">of</span>
            <Select value={state.of} onChange={(e) => onState({ of: e.target.value })} className="w-auto min-w-36" aria-label="Measured field">
              {OF_FIELDS.map((f) => (
                <option key={f} value={f}>
                  {fieldLabel(f)}
                </option>
              ))}
              {attrKeys.length > 0 && (
                <optgroup label="Details sent with messages (numbers)">
                  {attrKeys.map((k) => (
                    <option key={k} value={k}>
                      {fieldLabel(k)}
                    </option>
                  ))}
                </optgroup>
              )}
              {state.of && !OF_FIELDS.includes(state.of) && !attrKeys.includes(state.of) && <option value={state.of}>{fieldLabel(state.of)}</option>}
            </Select>
          </>
        )}
        <span className="ml-2 text-ink-3">split by</span>
        <Select value={state.groupBy} onChange={(e) => onState({ groupBy: e.target.value })} className="w-auto min-w-36" aria-label="Split by">
          <option value="">Nothing</option>
          {GROUP_FIELDS.map((f) => (
            <option key={f} value={f}>
              {fieldLabel(f)}
            </option>
          ))}
          {attrKeys.length > 0 && (
            <optgroup label="Details sent with messages">
              {attrKeys.map((k) => (
                <option key={k} value={k}>
                  {fieldLabel(k)}
                </option>
              ))}
            </optgroup>
          )}
          {state.groupBy && !GROUP_FIELDS.includes(state.groupBy) && !attrKeys.includes(state.groupBy) && <option value={state.groupBy}>{fieldLabel(state.groupBy)}</option>}
        </Select>
        {state.groupBy && (
          <>
            <span className="text-ink-3">top</span>
            <Select value={state.limit} onChange={(e) => onState({ limit: Number(e.target.value) })} className="w-auto" aria-label="How many groups">
              {LIMITS.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </Select>
          </>
        )}
        <UpdatingDot active={agg.isFetching && !!data} className="ml-1" />
        <button type="button" className="ml-auto text-xs text-ink-3 hover:text-ink" onClick={() => onState(DEFAULT_ANALYTICS)}>
          Reset
        </button>
      </div>
      {badQuery ? (
        <div role="alert" className="m-4 rounded-md border border-critical/40 bg-critical/[0.05] px-3 py-2 text-[13px] text-critical-text">
          {badQuery}
        </div>
      ) : agg.isError && !data ? (
        <ErrorState error={agg.error} retry={() => agg.refetch()} />
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto bg-surface">
          <div className="border-b border-border px-4 pb-2 pt-3">
            <div className="flex flex-wrap items-baseline gap-x-3 text-xs text-ink-3">
              <span className="text-[14px] font-semibold text-ink">{label}</span>
              {data && (
                <span>
                  {fmtNumber(total)} messages
                  {data.total.value !== null && state.metric !== "count" && <> · overall {fmt(data.total.value)}</>}
                  {groupedChartLimited && <> · the chart shows the top 8 of {data.groups.length}</>}
                </span>
              )}
            </div>
            {state.metric === "count" ? (
              <TimelineHistogram data={data?.histogram} loading={agg.isPending} height={220} onZoom={onZoom} className="mt-2" />
            ) : (
              <MultiLineChart buckets={data?.buckets ?? []} series={series} bucketSeconds={data?.bucket_seconds} height={220} format={fmt} loading={agg.isPending} className="mt-2" />
            )}
          </div>
          <DataTable
            label="Breakdown"
            columns={columns}
            rows={data ? (total === 0 ? [] : rows) : undefined}
            rowKey={(g) => (g.total ? "__total" : g.other ? "__other" : g.key)}
            loading={agg.isPending}
            rowClassName={(g) => (g.total ? "bg-surface-2/60" : undefined)}
            empty={<EmptyState title="No messages match" hint="Widen the time range or remove a filter." className="py-8" />}
            stickyHeader={false}
          />
        </div>
      )}
    </div>
  );
}
