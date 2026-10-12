import { useEffect, useMemo, useState } from "react";
import { useTraceFields, useTraces } from "@/api/hooks";
import { get, ApiRequestError } from "@/api/client";
import type { SpanNameStats, TraceSummary, TracesResponse } from "@/api/types";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { LatencyHistogram } from "@/components/charts/latency-histogram";
import { Badge, DataTable, EmptyState, LoadMore, MetaLine, Page, Section, Skeleton, Swatch, Term, type Column } from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { addTerm } from "@/lib/query";
import { fmtNumber, fmtPct, fmtTs, fmtWhen } from "@/lib/time";
import { pluralize } from "@/lib/format";
import { SearchBar } from "@/features/logs/search-bar";
import { fmtUs } from "./waterfall-layout";
import { serviceColor } from "./waterfall";
import { TRACE_SEARCH_EXAMPLES, requestLabel, requestResult } from "./trace-words";

const PAGE = 50;

export function TracesPage() {
  const { range, setRange } = useRange();
  const { get: g, set } = useUrlState();
  const q = g("q");
  const fields = useTraceFields();
  const traces = useTraces({ q, from: range.from, to: range.to, limit: PAGE, include: "stats" });
  const [older, setOlder] = useState<TraceSummary[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<unknown>(null);
  useEffect(() => {
    setOlder([]);
    setCursor(undefined);
    setMoreError(null);
  }, [traces.data]);
  const queryError = useMemo(() => {
    const e = traces.error;
    if (e instanceof ApiRequestError && e.status === 400) return { message: e.body.message || e.message, pos: e.body.pos };
    return null;
  }, [traces.error]);
  const items = useMemo(() => [...(traces.data?.items ?? []), ...older], [traces.data, older]);
  const next = cursor ?? traces.data?.next_cursor;
  const services = useMemo(() => Array.from(new Set(items.flatMap((t) => t.services))).sort(), [items]);
  const stats = traces.data?.stats;
  const setQuery = (nq: string) => set({ q: nq || undefined });
  const loadMore = async () => {
    if (!next || loadingMore) return;
    setLoadingMore(true);
    setMoreError(null);
    try {
      const r = await get<TracesResponse>("/traces", { q, from: range.from, to: range.to, limit: PAGE, cursor: next });
      setOlder((o) => [...o, ...r.items]);
      setCursor(r.next_cursor ?? "");
    } catch (e) {
      setMoreError(e);
    } finally {
      setLoadingMore(false);
    }
  };

  const opColumns: Column<SpanNameStats>[] = [
    {
      id: "name",
      header: "Operation",
      cell: (s) => (
        <button type="button" className="block w-full truncate text-left font-mono text-[12px] text-ink hover:text-accent" title={`Show only ${s.name}`} onClick={() => setQuery(addTerm(q, "name", s.name))}>
          {s.name}
        </button>
      ),
    },
    { id: "count", header: <Term k="span">Steps</Term>, width: "64px", align: "right", cell: (s) => <span className="text-ink-2">{fmtNumber(s.count)}</span> },
    { id: "p50", header: <Term k="p50">Typical</Term>, width: "72px", align: "right", cell: (s) => <span className="text-ink-2">{fmtUs(s.p50_us)}</span> },
    { id: "p95", header: <Term k="p95">Slow</Term>, width: "72px", align: "right", cell: (s) => <span className="text-ink">{fmtUs(s.p95_us)}</span> },
    { id: "failed", header: "Failed", width: "64px", align: "right", cell: (s) => <span className={s.error_pct >= 1 ? "font-medium text-critical-text" : "text-ink-3"}>{fmtPct(s.error_pct)}</span> },
  ];

  const columns: Column<TraceSummary>[] = [
    { id: "start", header: "Started", width: "150px", cell: (t) => <span className="tabular text-ink-2" title={fmtTs(t.start_ts, { ms: true })}>{fmtWhen(t.start_ts, { seconds: true })}</span> },
    {
      id: "app",
      header: <Term k="service">App</Term>,
      width: "minmax(100px,160px)",
      cell: (t) => (
        <span className="flex min-w-0 items-center gap-2">
          <Swatch color={serviceColor(services, t.service)} />
          <span className="truncate text-ink">{t.service}</span>
        </span>
      ),
    },
    {
      id: "request",
      header: "Request",
      cell: (t) => (
        <div className="min-w-0">
          <div className="truncate font-mono text-[12px] text-ink">{requestLabel(t)}</div>
          <MetaLine>
            {t.http_status ? `response ${t.http_status}` : ""}
            {t.services.length > 1 ? `also ${t.services.filter((s) => s !== t.service).join(", ")}` : ""}
            <span className="font-mono text-[11px]" title="Request timeline ID">
              {t.trace_id.slice(0, 12)}
            </span>
          </MetaLine>
        </div>
      ),
    },
    { id: "duration", header: "Duration", width: "90px", align: "right", cell: (t) => <span className="font-medium text-ink">{fmtUs(t.duration_us)}</span> },
    {
      id: "steps",
      header: <Term k="span">Steps</Term>,
      width: "90px",
      align: "right",
      cell: (t) => (
        <span className="text-ink-2">
          {t.span_count}
          {t.errors > 0 && <span className="ml-1 text-critical-text">({t.errors} failed)</span>}
        </span>
      ),
    },
    {
      id: "result",
      header: "Result",
      width: "84px",
      cell: (t) => {
        const r = requestResult(t);
        return r === "failed" ? (
          <Badge tone="critical">Failed</Badge>
        ) : r === "ok" ? (
          <span className="text-good-text">OK</span>
        ) : (
          <span className="text-ink-3" title="The app didn't report whether this request succeeded">
            —
          </span>
        );
      },
    },
  ];

  return (
    <Page
      width="full"
      title="Requests"
      description="Each request your apps handled, step by step (traces). Open one to see where the time went."
      actions={<TimeRangePicker value={range} onChange={setRange} onRefresh={() => traces.refetch()} refreshing={traces.isFetching} />}
      toolbar={
        <SearchBar
          value={q}
          onChange={setQuery}
          error={queryError}
          range={range}
          fields={fields.data?.fields ?? []}
          examples={TRACE_SEARCH_EXAMPLES}
          placeholder="Search requests, e.g. GET /orders — or filter with service:api status:error duration:>250"
          ariaLabel="Search requests"
        />
      }
    >
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="flex flex-col gap-4 px-5 py-4">
          {!queryError && (
            <div className="grid gap-4 lg:grid-cols-[2fr_3fr]">
              <Section title="How long requests take" description="How many requests finished within each duration range.">
                <LatencyHistogram buckets={stats?.latency} loading={traces.isPending} height={170} />
              </Section>
              <Section title="Busiest operations" description="The most frequent steps. Click one to show only it." flush>
                <div className="max-h-[214px] overflow-y-auto">
                  <DataTable label="Busiest operations" columns={opColumns} rows={stats?.by_name} rowKey={(s) => s.name} loading={traces.isPending} skeletonRows={4} empty={<EmptyState title="No steps in this time range" className="py-6" />} />
                </div>
              </Section>
            </div>
          )}
          <Section title="Requests" description={traces.data ? `${pluralize(items.length, "request")}${next ? " so far, newest first" : ", newest first"}` : undefined} flush>
            {queryError ? (
              <EmptyState title="Fix the search above to see requests" hint="Click the ? in the search box for examples of what you can type." />
            ) : traces.isPending ? (
              <Skeleton rows={10} className="p-4" />
            ) : (
              <DataTable
                label="Requests"
                columns={columns}
                rows={items}
                rowKey={(t) => t.trace_id}
                rowHref={(t) => `/traces/${t.trace_id}?at=${encodeURIComponent(t.start_ts)}`}
                error={traces.isError ? traces.error : undefined}
                onRetry={() => traces.refetch()}
                stickyHeader={false}
                empty={
                  <EmptyState
                    title="No requests in this time range"
                    hint={q ? "Nothing matches this search. Try a wider time range or remove a filter." : "Requests appear when your apps send traces (OpenTelemetry, or the SDK's FastAPI and Django integrations)."}
                  />
                }
                footer={items.length > 0 ? <LoadMore shown={items.length} noun="requests shown" hasMore={!!next} loading={loadingMore} error={moreError} onMore={loadMore} label="Show earlier" /> : undefined}
              />
            )}
          </Section>
        </div>
      </div>
    </Page>
  );
}
