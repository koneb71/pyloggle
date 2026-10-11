import { useMemo } from "react";
import { Link } from "react-router";
import { Users } from "lucide-react";
import { useErrorTrend, useLogVolume, useOverview, useRequestStats, useTopIssues } from "@/api/hooks";
import { PageHeader } from "@/components/shell/app-shell";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { TimelineHistogram } from "@/components/charts/timeline-histogram";
import { TrendChart } from "@/components/charts/trend-chart";
import { MultiLineChart } from "@/components/charts/multi-line";
import { Sparkline } from "@/components/charts/sparkline";
import { Badge, Card, CardHeader, EmptyState, ErrorState, LevelBadge, Skeleton, StatTile, StatusBadge, TimeAgo } from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { bucketLabel, fmtMs, fmtNumber, fmtPct } from "@/lib/time";
import { levelVar } from "@/lib/levels";

const DASH_DEFAULT = { from: "-24h", to: "" };

export function DashboardsPage() {
  const { range, setRange, zoom } = useRange(DASH_DEFAULT);
  const { get } = useUrlState();
  const scope = useMemo(() => ({ from: range.from, to: range.to, service: get("service") || undefined, env: get("env") || undefined }), [range.from, range.to, get]);
  const overview = useOverview(scope);
  const errors = useErrorTrend(scope);
  const volume = useLogVolume(scope);
  const top = useTopIssues(scope);
  const reqs = useRequestStats(scope);
  const o = overview.data?.overview;
  const errRate = o && o.events > 0 ? (o.errors / o.events) * 100 : 0;
  const rt = reqs.data?.totals;
  const reqSeries = useMemo(() => {
    const pts = reqs.data?.points ?? [];
    return {
      buckets: pts.map((p) => p.ts),
      traffic: [
        { name: "requests", values: pts.map((p) => p.requests), color: "var(--series-1)" },
        { name: "5xx", values: pts.map((p) => p.errors), color: "var(--lvl-error)" },
      ],
      latency: [
        { name: "p50", values: pts.map((p) => (p.requests > 0 ? p.p50_ms : null)), color: "var(--series-3)" },
        { name: "p95", values: pts.map((p) => (p.requests > 0 ? p.p95_ms : null)), color: "var(--series-1)" },
        { name: "p99", values: pts.map((p) => (p.requests > 0 ? p.p99_ms : null)), color: "var(--series-7)" },
      ],
    };
  }, [reqs.data]);

  return (
    <>
      <PageHeader title="Dashboards" sub="Project health at a glance">
        <TimeRangePicker value={range} onChange={setRange} onRefresh={() => (overview.refetch(), errors.refetch(), volume.refetch(), top.refetch(), reqs.refetch())} refreshing={overview.isFetching} />
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[1400px] flex-col gap-3 p-4">
          {overview.isError && <ErrorState error={overview.error} retry={() => overview.refetch()} />}
          <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
            <StatTile label="Log events" value={o ? fmtNumber(o.events) : "…"} hint="all levels, in range" />
            <StatTile label="Errors" value={o ? fmtNumber(o.errors) : "…"} hint={o ? `${errRate.toFixed(errRate < 1 ? 2 : 1)}% of events` : undefined} tone={o && o.errors > 0 ? "critical" : undefined} />
            <StatTile label="Open issues" value={o ? fmtNumber(o.open_issues) : "…"} hint="seen in range" />
            <StatTile label="New issues" value={o ? fmtNumber(o.new_issues) : "…"} hint="first seen in range" tone={o && o.new_issues > 0 ? "warning" : undefined} />
            <StatTile label="Affected users" value={o ? fmtNumber(o.affected_users) : "…"} hint="distinct users with errors" />
            <StatTile label="Services" value={o ? fmtNumber(o.services) : "…"} hint="reporting in range" />
          </div>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
            <StatTile label="Requests" value={rt ? fmtNumber(rt.requests) : "…"} hint={rt ? `${rt.rps.toFixed(rt.rps < 1 ? 2 : 1)} req/s over the range` : undefined} />
            <StatTile label="5xx rate" value={rt ? fmtPct(rt.error_pct) : "…"} hint={rt ? `${fmtNumber(rt.errors)} failed requests` : undefined} tone={rt && rt.error_pct >= 1 ? "critical" : rt && rt.error_pct > 0 ? "warning" : undefined} />
            <StatTile label="p95 latency" value={rt && rt.requests > 0 ? fmtMs(rt.p95_ms) : "…"} hint={rt && rt.requests > 0 ? `p50 ${fmtMs(rt.p50_ms)} · p99 ${fmtMs(rt.p99_ms)}` : "no request rows in range"} />
          </div>
          <div className="grid gap-3 lg:grid-cols-2">
            <Card>
              <CardHeader title="Error occurrences" sub={errors.data ? `${bucketLabel(errors.data.bucket_seconds)} buckets` : undefined} />
              <div className="p-2">
                {errors.isPending ? <Skeleton rows={4} className="p-2" /> : <TrendChart points={errors.data?.points} height={200} label="errors" />}
              </div>
            </Card>
            <Card>
              <CardHeader title="Log volume by level" sub={volume.data ? `${bucketLabel(volume.data.bucket_seconds)} buckets · drag to zoom` : undefined} />
              <div className="p-2">
                <TimelineHistogram data={volume.data} loading={volume.isPending} height={200} onZoom={zoom} />
              </div>
            </Card>
          </div>
          <div className="grid gap-3 lg:grid-cols-2">
            <Card>
              <CardHeader title="Requests and 5xx" sub={reqs.data ? `${bucketLabel(reqs.data.bucket_seconds)} buckets · from request_stats_minutely` : undefined} />
              <div className="p-2">
                <MultiLineChart buckets={reqSeries.buckets} series={reqSeries.traffic} bucketSeconds={reqs.data?.bucket_seconds} height={200} loading={reqs.isPending} emptyText="No request rows yet (Flask/FastAPI integrations and OTLP server spans feed this)" />
              </div>
            </Card>
            <Card>
              <CardHeader title="Latency" sub="p50 / p95 / p99 of request duration" />
              <div className="p-2">
                <MultiLineChart buckets={reqSeries.buckets} series={reqSeries.latency} bucketSeconds={reqs.data?.bucket_seconds} height={200} format={fmtMs} loading={reqs.isPending} emptyText="No timed requests in range" />
              </div>
            </Card>
          </div>
          <Card>
            <CardHeader title="Latency by route" sub="busiest routes in range" actions={<Link to="/logs?view=analytics&group_by_a=http.route&metric=p95" className="text-xs text-accent hover:underline">Analytics →</Link>} />
            {reqs.isPending ? (
              <Skeleton rows={4} className="p-3" />
            ) : (reqs.data?.routes.length ?? 0) === 0 ? (
              <EmptyState title="No request rows in this range" hint="Request stats come from the SDK request events and OTLP server spans." className="py-6" />
            ) : (
              <table className="w-full text-[13px]">
                <thead className="text-left text-[11px] uppercase tracking-wide text-ink-3">
                  <tr className="border-b border-border">
                    <th className="px-3 py-1.5 font-medium">route</th>
                    <th className="px-3 py-1.5 text-right font-medium">requests</th>
                    <th className="px-3 py-1.5 text-right font-medium">5xx</th>
                    <th className="px-3 py-1.5 text-right font-medium">p50</th>
                    <th className="px-3 py-1.5 text-right font-medium">p95</th>
                    <th className="px-3 py-1.5 text-right font-medium">p99</th>
                  </tr>
                </thead>
                <tbody>
                  {reqs.data!.routes.map((r) => (
                    <tr key={`${r.method} ${r.route}`} className="border-t border-border hover:bg-surface-2">
                      <td className="max-w-0 px-3 py-1.5">
                        <Link to={`/logs?q=${encodeURIComponent(`http.route:${JSON.stringify(r.route ?? "")} http.method:${r.method ?? ""}`)}&from=${encodeURIComponent(range.from)}${range.to ? `&to=${encodeURIComponent(range.to)}` : ""}`} className="block truncate font-mono text-ink hover:text-accent" title={`${r.method} ${r.route}`}>
                          <span className="text-ink-3">{r.method}</span> {r.route}
                        </Link>
                      </td>
                      <td className="px-3 py-1.5 text-right tabular text-ink">{fmtNumber(r.requests)}</td>
                      <td className={`px-3 py-1.5 text-right tabular ${r.error_pct >= 1 ? "text-critical" : "text-ink-2"}`}>{fmtPct(r.error_pct)}</td>
                      <td className="px-3 py-1.5 text-right tabular text-ink-2">{fmtMs(r.p50_ms)}</td>
                      <td className="px-3 py-1.5 text-right tabular text-ink">{fmtMs(r.p95_ms)}</td>
                      <td className="px-3 py-1.5 text-right tabular text-ink-2">{fmtMs(r.p99_ms)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </Card>
          <Card>
            <CardHeader title="Top issues" sub="by occurrences in range" actions={<Link to="/issues" className="text-xs text-accent hover:underline">All issues →</Link>} />
            {top.isPending ? (
              <Skeleton rows={5} className="p-3" />
            ) : (top.data?.items.length ?? 0) === 0 ? (
              <EmptyState title="No errors in this range" hint="Nice. Issues will show up here as soon as something breaks." className="py-8" />
            ) : (
              <table className="w-full text-[13px]">
                <tbody>
                  {top.data!.items.map((it) => (
                    <tr key={it.issue_id} className="border-t border-border hover:bg-surface-2">
                      <td className="w-full px-3 py-2">
                        <div className="flex items-center gap-2">
                          <span className="lvl-bar h-5" style={{ background: levelVar(it.level) }} />
                          <Link to={`/issues/${it.issue_id}`} className="truncate font-medium text-ink hover:text-accent">
                            {it.title || it.exception_type}
                          </Link>
                          <StatusBadge status={it.status} regressed={it.is_regressed} />
                        </div>
                        <div className="mt-0.5 flex flex-wrap items-center gap-2 pl-4 text-xs text-ink-3">
                          <span className="font-mono text-ink-2">{it.culprit}</span>
                          <LevelBadge level={it.level} />
                          <Badge>{it.service}</Badge>
                          {it.env && <Badge tone="outline">{it.env}</Badge>}
                        </div>
                      </td>
                      <td className="px-2 py-2">
                        <Sparkline values={it.sparkline} color={levelVar(it.level)} />
                      </td>
                      <td className="px-2 py-2 text-right font-medium tabular text-ink">{fmtNumber(it.occurrence_count)}</td>
                      <td className="px-2 py-2 text-right tabular text-ink-2">
                        <span className="inline-flex items-center gap-1">
                          <Users size={11} className="text-ink-3" />
                          {fmtNumber(it.affected_users)}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-right text-xs text-ink-3">
                        <TimeAgo iso={it.last_seen} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </Card>
        </div>
      </div>
    </>
  );
}
