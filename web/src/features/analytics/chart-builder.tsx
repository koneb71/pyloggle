import { useMemo } from "react";
import clsx from "clsx";
import { useChartAggregate, useLogFacets } from "@/api/hooks";
import type { AggregateGroup, LogAggregate } from "@/api/types";
import { ApiRequestError } from "@/api/client";
import { MultiLineChart } from "@/components/charts/multi-line";
import { TimelineHistogram } from "@/components/charts/timeline-histogram";
import { ShareBar } from "@/components/charts/share-bar";
import { DataTable, EmptyState, ErrorState, Select, Swatch, UpdatingDot, type Column } from "@/components/ui";
import { fmtNumber } from "@/lib/time";
import { levelLabel, levelVar } from "@/lib/levels";
import { fieldLabel, isCodeLike } from "@/lib/fields";
import { SERIES_VARS } from "@/components/charts/palette";
import { chartSeries, DEFAULT_CHART, GROUP_FIELDS, METRICS, metricFormat, metricLabel, metricWord, needsField, OF_FIELDS, sharePct, SOURCE_WORDS, stacks, type ChartState, type Metric } from "./chart-model";

export interface ChartScope {
  from: string;
  to: string;
  service?: string;
  env?: string;
}

const LIMITS = [5, 8, 15, 25];

type Row = AggregateGroup & { color: string; other: boolean; total?: boolean };

/** The data of a chart: what ChartBuilder and pinned charts on Dashboards draw. */
export function useChart(state: ChartState, scope: ChartScope, buckets = 60) {
  return useChartAggregate({
    source: state.source,
    q: state.q || undefined,
    from: scope.from,
    to: scope.to || undefined,
    service: scope.service || undefined,
    env: scope.env || undefined,
    group_by: state.groupBy,
    metric: state.metric,
    of: needsField(state.source, state.metric) ? state.of : undefined,
    limit: state.limit,
    buckets,
  });
}

/** The chart drawn: stacked bars for counts, a line per group for everything else. */
export function ChartPlot({ state, data, loading, height = 220, onZoom, className }: { state: ChartState; data?: LogAggregate; loading?: boolean; height?: number; onZoom?: (from: Date, to: Date) => void; className?: string }) {
  const series = useMemo(() => chartSeries(data, state.source), [data, state.source]);
  if (stacks(state.metric)) return <TimelineHistogram data={data?.histogram} loading={loading} height={height} onZoom={onZoom} className={className} />;
  const fmt = metricFormat(state.source, state.metric, state.of);
  return <MultiLineChart buckets={data?.buckets ?? []} series={series} bucketSeconds={data?.bucket_seconds} height={height} format={fmt} loading={loading} className={className} emptyText={`No ${SOURCE_WORDS[state.source].many} in this time range`} />;
}

/** Charts: count or measure messages, errors or requests over time, split by a field. */
export function ChartBuilder({ state, scope, onState, onFilter, onZoom }: { state: ChartState; scope: ChartScope; onState: (patch: Partial<ChartState>) => void; onFilter: (field: string, value: string) => void; onZoom?: (from: Date, to: Date) => void }) {
  const { source } = state;
  const words = SOURCE_WORDS[source];
  // Attribute keys come from the messages' details; errors and requests offer their named fields.
  const facets = useLogFacets({ q: state.q, from: scope.from, to: scope.to || undefined, fields: ["service"], attr_keys: 40 }, source === "messages");
  const attrKeys = useMemo(() => (source === "messages" ? (facets.data?.attr_keys ?? []).map((k) => `attrs.${k.value}`) : []), [facets.data, source]);
  const agg = useChart(state, scope);
  const data = agg.data;
  const measures = needsField(source, state.metric);
  const fmt = metricFormat(source, state.metric, state.of);
  const label = metricLabel(source, state.metric, measures ? state.of : undefined);
  const badQuery = agg.error instanceof ApiRequestError && agg.error.status === 400 ? agg.error.body.message || agg.error.message : null;
  const total = data?.total.rows ?? 0;
  const groupLabel = data?.group_by ? fieldLabel(data.group_by) : "";
  const showValue = state.metric !== "count";
  const rows = useMemo<Row[]>(() => {
    if (!data) return [];
    const list: Row[] = data.groups.map((g, i) => ({ ...g, color: data.group_by === "level" ? levelVar(g.key) : SERIES_VARS[i % SERIES_VARS.length], other: false }));
    if (data.other) list.push({ ...data.other, color: "var(--lvl-other)", other: true });
    if (data.group_by) list.push({ ...data.total, key: "__total", color: "", other: false, total: true });
    return list;
  }, [data]);
  const groupedChartLimited = data && data.groups.length > 8;
  const groups = GROUP_FIELDS[source];
  const ofFields = OF_FIELDS[source];

  const columns: Column<Row>[] = [
    {
      id: "group",
      header: groupLabel || `All ${words.many}`,
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
              <span className="text-ink">All {words.many}</span>
            )}
          </span>
        ),
    },
    { id: "rows", header: words.label, width: "110px", align: "right", hideBelow: showValue ? "sm" : undefined, cell: (g) => <span className={g.total ? "font-medium text-ink" : "text-ink-2"}>{fmtNumber(g.rows)}</span> },
    {
      id: "share",
      header: "Share",
      width: "180px",
      hideBelow: "lg",
      cell: (g) => (g.total ? <span className="w-full text-right text-xs text-ink-3">100%</span> : <ShareBar pct={sharePct(g.rows, total)} color={g.color} />),
    },
    ...(showValue ? [{ id: "value", header: label, width: "150px", align: "right" as const, cell: (g: Row) => <span className={clsx("font-medium", g.value === null ? "text-ink-3" : "text-ink")}>{g.value === null ? "—" : fmt(g.value)}</span> }] : []),
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-none flex-wrap items-center gap-x-2 gap-y-2 border-b border-border bg-surface px-4 py-2.5 text-[13px]">
        <span className="text-ink-3">Show</span>
        <Select value={state.metric} onChange={(e) => onState({ metric: e.target.value as Metric })} className="w-auto min-w-40" aria-label="What to show">
          {METRICS[source].map((m) => (
            <option key={m} value={m}>
              {metricWord(source, m)}
            </option>
          ))}
        </Select>
        {measures && (
          <>
            <span className="text-ink-3">of</span>
            <Select value={state.of} onChange={(e) => onState({ of: e.target.value })} className="w-auto min-w-36" aria-label="Measured field">
              {ofFields.map((f) => (
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
              {state.of && !ofFields.includes(state.of) && !attrKeys.includes(state.of) && <option value={state.of}>{fieldLabel(state.of)}</option>}
            </Select>
          </>
        )}
        <span className="ml-2 text-ink-3">split by</span>
        <Select value={state.groupBy} onChange={(e) => onState({ groupBy: e.target.value })} className="w-auto min-w-36" aria-label="Split by">
          <option value="">Nothing</option>
          {groups.map((f) => (
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
          {state.groupBy && !groups.includes(state.groupBy) && !attrKeys.includes(state.groupBy) && <option value={state.groupBy}>{fieldLabel(state.groupBy)}</option>}
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
        <button type="button" className="ml-auto text-xs text-ink-3 hover:text-ink" onClick={() => onState({ ...DEFAULT_CHART[source], q: state.q })}>
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
        <div className="min-h-0 flex-1 bg-surface">
          <div className="border-b border-border px-4 pb-2 pt-3">
            <div className="flex flex-wrap items-baseline gap-x-3 text-xs text-ink-3">
              <span className="text-[14px] font-semibold text-ink">{label}</span>
              {data && (
                <span>
                  {fmtNumber(total)} {total === 1 ? words.one : words.many}
                  {data.total.value !== null && showValue && <> · overall {fmt(data.total.value)}</>}
                  {groupedChartLimited && <> · the chart shows the top 8 of {data.groups.length}</>}
                </span>
              )}
            </div>
            <ChartPlot state={state} data={data} loading={agg.isPending} onZoom={onZoom} className="mt-2" />
          </div>
          <DataTable
            label="Breakdown"
            columns={columns}
            rows={data ? (total === 0 ? [] : rows) : undefined}
            rowKey={(g) => (g.total ? "__total" : g.other ? "__other" : g.key)}
            loading={agg.isPending}
            rowClassName={(g) => (g.total ? "bg-surface-2/60" : undefined)}
            empty={<EmptyState title={`No ${words.many} match`} hint={source === "requests" ? "Widen the time range or remove a filter. Requests are counted by the Python package's web integrations and by OpenTelemetry." : "Widen the time range or remove a filter."} className="py-8" />}
            stickyHeader={false}
          />
        </div>
      )}
    </div>
  );
}
