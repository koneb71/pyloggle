import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { AlertTriangle } from "lucide-react";
import { useTraceFields, useTraces } from "@/api/hooks";
import { get, ApiRequestError } from "@/api/client";
import type { TraceSummary, TracesResponse } from "@/api/types";
import { PageHeader } from "@/components/shell/app-shell";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { LatencyHistogram } from "@/components/charts/latency-histogram";
import { Badge, Button, Card, CardHeader, EmptyState, ErrorState, Skeleton, TimeAgo } from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { addTerm } from "@/lib/query";
import { fmtNumber, fmtPct, fmtTs } from "@/lib/time";
import { SearchBar } from "@/features/logs/search-bar";
import { fmtUs } from "./waterfall-layout";
import { serviceColor } from "./waterfall";

const PAGE = 50;

export function TracesPage() {
  const { range, setRange } = useRange();
  const { get: g, set } = useUrlState();
  const q = g("q");
  const nav = useNavigate();
  const fields = useTraceFields();
  const traces = useTraces({ q, from: range.from, to: range.to, limit: PAGE, include: "stats" });
  const [older, setOlder] = useState<TraceSummary[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  const queryError = useMemo(() => {
    const e = traces.error;
    if (e instanceof ApiRequestError && e.status === 400) return { message: e.body.message || e.message, pos: e.body.pos };
    return null;
  }, [traces.error]);
  const items = useMemo(() => [...(traces.data?.items ?? []), ...older], [traces.data, older]);
  const next = cursor ?? traces.data?.next_cursor;
  const services = useMemo(() => Array.from(new Set(items.flatMap((t) => t.services))).sort(), [items]);
  const stats = traces.data?.stats;
  const setQuery = (nq: string) => {
    setOlder([]);
    setCursor(undefined);
    set({ q: nq || undefined });
  };
  const loadMore = async () => {
    if (!next || loadingMore) return;
    setLoadingMore(true);
    try {
      const r = await get<TracesResponse>("/traces", { q, from: range.from, to: range.to, limit: PAGE, cursor: next });
      setOlder((o) => [...o, ...r.items]);
      setCursor(r.next_cursor ?? "");
    } finally {
      setLoadingMore(false);
    }
  };
  const open = (t: TraceSummary) => nav(`/traces/${t.trace_id}?at=${encodeURIComponent(t.start_ts)}`);

  return (
    <>
      <PageHeader title="Traces" sub={traces.data ? `${fmtNumber(items.length)}${next ? "+" : ""} traces in range` : undefined}>
        <TimeRangePicker value={range} onChange={setRange} onRefresh={() => traces.refetch()} refreshing={traces.isFetching} />
      </PageHeader>
      <div className="flex-none border-b border-border bg-surface px-4 py-2">
        <SearchBar value={q} onChange={setQuery} error={queryError} range={range} fields={fields.data?.fields} placeholder='Search spans… e.g. service:api status:error duration:>250 "GET /orders"' ariaLabel="Trace query" />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[1400px] flex-col gap-3 p-4">
          <div className="grid gap-3 lg:grid-cols-[2fr_3fr]">
            <Card>
              <CardHeader title="Latency distribution" sub="entry spans, power-of-two buckets" />
              <div className="p-2">
                <LatencyHistogram buckets={stats?.latency} loading={traces.isPending} height={160} />
              </div>
            </Card>
            <Card>
              <CardHeader title="By span name" sub="busiest operations · click to filter" />
              {traces.isPending ? (
                <Skeleton rows={4} className="p-3" />
              ) : (stats?.by_name.length ?? 0) === 0 ? (
                <EmptyState title="No spans" className="py-6" />
              ) : (
                <div className="max-h-44 overflow-y-auto">
                  <table className="w-full text-xs">
                    <thead className="text-left text-[10px] uppercase tracking-wide text-ink-3">
                      <tr className="border-b border-border">
                        <th className="px-3 py-1 font-medium">name</th>
                        <th className="px-2 py-1 text-right font-medium">spans</th>
                        <th className="px-2 py-1 text-right font-medium">p50</th>
                        <th className="px-2 py-1 text-right font-medium">p95</th>
                        <th className="px-3 py-1 text-right font-medium">errors</th>
                      </tr>
                    </thead>
                    <tbody>
                      {stats!.by_name.map((s) => (
                        <tr key={s.name} className="border-b border-border hover:bg-surface-2">
                          <td className="max-w-0 px-3 py-1">
                            <button type="button" className="block w-full truncate text-left font-mono text-ink hover:text-accent" title={s.name} onClick={() => setQuery(addTerm(q, "name", s.name))}>
                              {s.name}
                            </button>
                          </td>
                          <td className="px-2 py-1 text-right tabular text-ink-2">{fmtNumber(s.count)}</td>
                          <td className="px-2 py-1 text-right tabular text-ink-2">{fmtUs(s.p50_us)}</td>
                          <td className="px-2 py-1 text-right tabular text-ink">{fmtUs(s.p95_us)}</td>
                          <td className={`px-3 py-1 text-right tabular ${s.error_pct >= 1 ? "text-critical" : "text-ink-3"}`}>{fmtPct(s.error_pct)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Card>
          </div>
          <Card>
            {traces.isError && !queryError ? (
              <ErrorState error={traces.error} retry={() => traces.refetch()} />
            ) : traces.isPending ? (
              <Skeleton rows={10} className="p-4" />
            ) : items.length === 0 ? (
              <EmptyState title="No traces" hint={q ? "Try widening the time range or removing a filter." : "Send OTLP traces to /v1/traces with your project's API key; spans are kept for a week by default."} />
            ) : (
              <table className="w-full text-[13px]">
                <thead className="text-left text-[11px] uppercase tracking-wide text-ink-3">
                  <tr className="border-b border-border">
                    <th className="px-3 py-1.5 font-medium">started</th>
                    <th className="px-3 py-1.5 font-medium">service</th>
                    <th className="w-full px-3 py-1.5 font-medium">trace</th>
                    <th className="px-3 py-1.5 text-right font-medium">duration</th>
                    <th className="px-3 py-1.5 text-right font-medium">spans</th>
                    <th className="px-3 py-1.5 font-medium">status</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((t) => {
                    const label = t.http_method && t.http_route ? `${t.http_method} ${t.http_route}` : t.name;
                    const failed = t.status === "error" || t.errors > 0;
                    return (
                      <tr key={t.trace_id} className="cursor-pointer border-b border-border hover:bg-surface-2" onClick={() => open(t)}>
                        <td className="whitespace-nowrap px-3 py-1.5 tabular text-ink-2" title={fmtTs(t.start_ts, { ms: true })}>
                          <TimeAgo iso={t.start_ts} />
                        </td>
                        <td className="whitespace-nowrap px-3 py-1.5">
                          <span className="inline-flex items-center gap-1.5">
                            <span className="inline-block h-2.5 w-2.5 rounded-sm" style={{ background: serviceColor(services, t.service) }} aria-hidden />
                            {t.service}
                          </span>
                        </td>
                        <td className="max-w-0 px-3 py-1.5">
                          <Link to={`/traces/${t.trace_id}?at=${encodeURIComponent(t.start_ts)}`} className="block truncate font-mono text-ink hover:text-accent" onClick={(e) => e.stopPropagation()}>
                            {label}
                          </Link>
                          <div className="mt-0.5 flex flex-wrap items-center gap-1 text-[11px] text-ink-3">
                            <span className="font-mono">{t.trace_id.slice(0, 16)}</span>
                            {t.services.filter((s) => s !== t.service).map((s) => (
                              <Badge key={s}>{s}</Badge>
                            ))}
                            {t.http_status ? <Badge tone="outline">{t.http_status}</Badge> : null}
                          </div>
                        </td>
                        <td className="whitespace-nowrap px-3 py-1.5 text-right font-medium tabular text-ink">{fmtUs(t.duration_us)}</td>
                        <td className="whitespace-nowrap px-3 py-1.5 text-right tabular text-ink-2">
                          {t.span_count}
                          {t.errors > 0 && <span className="ml-1 text-critical">({t.errors} failed)</span>}
                        </td>
                        <td className="whitespace-nowrap px-3 py-1.5">
                          {failed ? (
                            <span className="inline-flex items-center gap-1 text-xs text-critical">
                              <AlertTriangle size={12} /> error
                            </span>
                          ) : (
                            <span className="text-xs text-ink-3">{t.status}</span>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
            {items.length > 0 && (
              <div className="flex items-center justify-center gap-2 py-2 text-[11px] text-ink-3">
                {items.length.toLocaleString()} loaded
                {next && (
                  <Button size="xs" onClick={loadMore} loading={loadingMore}>
                    Load older
                  </Button>
                )}
              </div>
            )}
          </Card>
        </div>
      </div>
    </>
  );
}
