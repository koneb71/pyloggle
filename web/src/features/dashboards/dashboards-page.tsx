import { useMemo } from "react";
import { ArrowRight, Check, ChevronDown } from "lucide-react";
import { useErrorTrend, useLogFacets, useLogVolume, useMetricNames, useOverview, useRequestStats, useTopIssues } from "@/api/hooks";
import type { IssueSummary, RouteStats } from "@/api/types";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { TimelineHistogram } from "@/components/charts/timeline-histogram";
import { TrendChart } from "@/components/charts/trend-chart";
import { MultiLineChart } from "@/components/charts/multi-line";
import { Sparkline } from "@/components/charts/sparkline";
import { Button, DataTable, EmptyState, ErrorState, LinkButton, MenuItem, MetaLine, Page, Popover, Section, Select, StatTile, Term, TimeAgo, type Column } from "@/components/ui";
import { MetricCard } from "./metric-card";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { fmtMs, fmtNumber, fmtPct, previousLabel } from "@/lib/time";
import { pluralize } from "@/lib/format";
import { issueHeadline } from "@/features/issues/issue-text";

const DASH_DEFAULT = { from: "-24h", to: "" };

/** Where the web-request numbers come from. "auto" uses request events and falls back to OpenTelemetry metrics. */
type Source = "auto" | "logs" | "otel";

export function DashboardsPage() {
  const { range, setRange, zoom } = useRange(DASH_DEFAULT);
  const { get, set } = useUrlState();
  const service = get("service");
  const env = get("env");
  const scope = useMemo(() => ({ from: range.from, to: range.to || undefined, service: service || undefined, env: env || undefined }), [range.from, range.to, service, env]);
  const vs = previousLabel(range);
  const metricNames = useMetricNames();
  const hasOtelHTTP = (metricNames.data?.items ?? []).some((m) => m.name === "http.server.request.duration");
  const chosen: Source = get("req_src") === "otel" ? "otel" : get("req_src") === "logs" ? "logs" : "auto";
  const overview = useOverview({ ...scope, compare: "previous" });
  const errors = useErrorTrend({ ...scope, buckets: 60 });
  const volume = useLogVolume(scope);
  const top = useTopIssues(scope);
  const fromLogs = useRequestStats({ ...scope, source: "logs", compare: "previous" }, chosen !== "otel");
  const autoOtel = chosen === "auto" && hasOtelHTTP && fromLogs.data?.totals.requests === 0;
  const fromOtel = useRequestStats({ ...scope, source: "otel", compare: "previous" }, chosen === "otel" || autoOtel);
  const source: "logs" | "otel" = chosen === "otel" || autoOtel ? "otel" : "logs";
  const reqs = source === "otel" ? fromOtel : fromLogs;
  const facets = useLogFacets({ from: range.from, to: range.to || undefined, fields: ["service", "env"], attr_keys: 0 });

  const o = overview.data?.overview;
  const prev = overview.data?.previous;
  const rt = reqs.data?.totals;
  const prt = reqs.data?.previous_totals;
  const hasReq = !!rt && rt.requests > 0;
  const reqSeries = useMemo(() => {
    const pts = reqs.data?.points ?? [];
    return {
      buckets: pts.map((p) => p.ts),
      traffic: [
        { name: "Requests", values: pts.map((p) => p.requests), color: "var(--series-1)" },
        { name: "Failed requests", values: pts.map((p) => p.errors), color: "var(--lvl-error)" },
      ],
      latency: [
        { name: "Typical (p50)", values: pts.map((p) => (p.requests > 0 ? p.p50_ms : null)), color: "var(--series-3)" },
        { name: "Slow (p95)", values: pts.map((p) => (p.requests > 0 ? p.p95_ms : null)), color: "var(--series-1)" },
        { name: "Slowest (p99)", values: pts.map((p) => (p.requests > 0 ? p.p99_ms : null)), color: "var(--series-7)" },
      ],
    };
  }, [reqs.data]);
  const refetch = () => [overview, errors, volume, top, reqs].forEach((q) => q.refetch());
  const rangeQs = `from=${encodeURIComponent(range.from)}${range.to ? `&to=${encodeURIComponent(range.to)}` : ""}`;

  const topColumns: Column<IssueSummary>[] = [
    {
      id: "issue",
      header: "Problem",
      cell: (it) => {
        const { headline, message } = issueHeadline(it);
        return (
          <div className="min-w-0">
            <div className="truncate">
              <span className="font-semibold text-ink">{headline}</span>
              {message && <span className="ml-2 text-ink-2">{message}</span>}
            </div>
            <MetaLine>
              {it.service}
              {it.env}
              <span>
                last seen <TimeAgo iso={it.last_seen} />
              </span>
            </MetaLine>
          </div>
        );
      },
    },
    { id: "trend", header: "Last 24 hours", width: "112px", hideBelow: "md", cell: (it) => <Sparkline values={it.sparkline} width={104} height={22} title="Times per hour, last 24 hours" /> },
    {
      id: "times",
      header: <Term k="events">Times</Term>,
      width: "80px",
      align: "right",
      cell: (it) => (
        <span className="flex flex-col items-end">
          <span className="font-medium text-ink">{fmtNumber(it.occurrence_count)}</span>
          <span className="whitespace-nowrap text-xs text-ink-3 sm:hidden">{pluralize(it.affected_users, "person", "people")}</span>
        </span>
      ),
    },
    { id: "people", header: <Term k="users">People</Term>, width: "72px", align: "right", hideBelow: "sm", cell: (it) => <span className="text-ink-2">{fmtNumber(it.affected_users)}</span> },
  ];
  const routeColumns: Column<RouteStats>[] = [
    {
      id: "route",
      header: "Endpoint",
      cell: (r) => (
        <span className="truncate font-mono text-[12px] text-ink" title={`${r.method} ${r.route}`}>
          <span className="text-ink-3">{r.method}</span> {r.route}
        </span>
      ),
    },
    { id: "requests", header: "Requests", width: "96px", align: "right", hideBelow: "sm", cell: (r) => <span className="text-ink">{fmtNumber(r.requests)}</span> },
    { id: "failed", header: <Term k="failed">Failed</Term>, width: "72px", align: "right", cell: (r) => <span className={r.error_pct >= 5 ? "font-medium text-critical-text" : r.error_pct >= 1 ? "text-warning-text" : "text-ink-3"}>{fmtPct(r.error_pct)}</span> },
    { id: "p50", header: <Term k="p50">Typical</Term>, width: "80px", align: "right", hideBelow: "md", cell: (r) => <span className="text-ink-2">{fmtMs(r.p50_ms)}</span> },
    { id: "p95", header: <Term k="p95">Slow</Term>, width: "72px", align: "right", cell: (r) => <span className="text-ink">{fmtMs(r.p95_ms)}</span> },
    { id: "p99", header: <Term k="p99">Slowest</Term>, width: "80px", align: "right", hideBelow: "md", cell: (r) => <span className="text-ink-2">{fmtMs(r.p99_ms)}</span> },
  ];

  return (
    <Page
      title="Dashboards"
      description="How this project is doing over time."
      actions={<TimeRangePicker value={range} onChange={setRange} onRefresh={refetch} refreshing={overview.isFetching || reqs.isFetching} />}
      toolbar={
        <div className="flex flex-wrap items-center gap-2 text-[13px] text-ink-3">
          <span>For</span>
          <Select value={service} onChange={(e) => set({ service: e.target.value || undefined })} className="w-auto min-w-32" aria-label="App">
            <option value="">All apps</option>
            {(facets.data?.fields.service ?? []).filter((f) => f.value).map((f) => (
              <option key={f.value} value={f.value}>
                {f.value}
              </option>
            ))}
            {service && !(facets.data?.fields.service ?? []).some((f) => f.value === service) && <option value={service}>{service}</option>}
          </Select>
          <span>in</span>
          <Select value={env} onChange={(e) => set({ env: e.target.value || undefined })} className="w-auto min-w-36" aria-label="Environment">
            <option value="">All environments</option>
            {(facets.data?.fields.env ?? []).filter((f) => f.value).map((f) => (
              <option key={f.value} value={f.value}>
                {f.value}
              </option>
            ))}
            {env && !(facets.data?.fields.env ?? []).some((f) => f.value === env) && <option value={env}>{env}</option>}
          </Select>
        </div>
      }
    >
      <Section
        title="Errors"
        description="Problems your apps reported."
        actions={
          <LinkButton to={`/issues?${rangeQs}`} size="xs" variant="ghost" icon={<ArrowRight size={13} />}>
            Issues
          </LinkButton>
        }
      >
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <StatTile label="Errors" value={o ? fmtNumber(o.errors) : ""} loading={overview.isPending} error={overview.isError && !o} delta={o && prev ? { current: o.errors, previous: prev.errors, goodWhen: "down", label: vs } : undefined} className="bg-surface-2/50" />
          <StatTile label={<Term k="unresolved">Unresolved issues</Term>} value={o ? fmtNumber(o.open_issues) : ""} loading={overview.isPending} error={overview.isError && !o} hint="that happened in this time range" className="bg-surface-2/50" />
          <StatTile label="New issues" value={o ? fmtNumber(o.new_issues) : ""} loading={overview.isPending} error={overview.isError && !o} delta={o && prev ? { current: o.new_issues, previous: prev.new_issues, goodWhen: "down", label: vs } : undefined} className="bg-surface-2/50" />
          <StatTile label={<Term k="users">People affected</Term>} value={o ? fmtNumber(o.affected_users) : ""} loading={overview.isPending} error={overview.isError && !o} delta={o && prev ? { current: o.affected_users, previous: prev.affected_users, goodWhen: "down", label: vs } : undefined} className="bg-surface-2/50" />
        </div>
        <div className="mt-5">
          <h4 className="mb-2 text-[13px] font-semibold text-ink">Errors over time</h4>
          {errors.isError && !errors.data ? <ErrorState compact error={errors.error} retry={() => errors.refetch()} title="Couldn't load the chart" /> : <TrendChart points={errors.data?.points} height={180} label="errors" variant="bars" />}
        </div>
        <div className="-mx-4 -mb-4 mt-5 border-t border-border">
          <div className="px-4 pb-1 pt-3 text-[13px] font-semibold text-ink">Most frequent problems</div>
          <DataTable
            label="Most frequent problems"
            columns={topColumns}
            rows={top.data?.items}
            rowKey={(it) => it.issue_id}
            rowHref={(it) => `/issues/${it.issue_id}?${rangeQs}`}
            loading={top.isPending}
            error={top.isError ? top.error : undefined}
            onRetry={() => top.refetch()}
            skeletonRows={5}
            stickyHeader={false}
            empty={<EmptyState title="No errors in this time range" hint="Problems show up here as soon as an app reports one." className="py-8" />}
          />
        </div>
      </Section>

      <Section
        title="Web requests"
        description="How much traffic your apps handled, how much failed, and how fast they answered."
        actions={<SourceMenu chosen={chosen} effective={source} hasOtel={hasOtelHTTP} onChange={(v) => set({ req_src: v === "auto" ? undefined : v })} />}
      >
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <StatTile label="Requests" value={rt ? fmtNumber(rt.requests) : ""} loading={reqs.isPending} error={reqs.isError && !rt} hint={hasReq ? `${rt.rps >= 1 ? rt.rps.toFixed(1) : (rt.rps * 60).toFixed(1)} per ${rt.rps >= 1 ? "second" : "minute"}` : "None recorded"} delta={rt && prt ? { current: rt.requests, previous: prt.requests, goodWhen: "neither", label: vs } : undefined} className="bg-surface-2/50" />
          <StatTile
            label={<Term k="failed">Failed requests</Term>}
            value={hasReq ? fmtPct(rt.error_pct) : "—"}
            tone={hasReq ? (rt.error_pct >= 5 ? "critical" : rt.error_pct >= 1 ? "warning" : undefined) : undefined}
            loading={reqs.isPending}
            error={reqs.isError && !rt}
            hint={hasReq ? `${fmtNumber(rt.errors)} of ${fmtNumber(rt.requests)}` : undefined}
            delta={hasReq && prt && prt.requests > 0 ? { current: rt.error_pct, previous: prt.error_pct, goodWhen: "down", label: vs } : undefined}
            className="bg-surface-2/50"
          />
          <StatTile label={<Term k="p50">Typical response time</Term>} value={hasReq ? fmtMs(rt.p50_ms) : "—"} loading={reqs.isPending} error={reqs.isError && !rt} delta={hasReq && prt && prt.requests > 0 ? { current: rt.p50_ms, previous: prt.p50_ms, goodWhen: "down", label: vs } : undefined} className="bg-surface-2/50" />
          <StatTile label={<Term k="p95">Slow requests take</Term>} value={hasReq ? fmtMs(rt.p95_ms) : "—"} loading={reqs.isPending} error={reqs.isError && !rt} hint={hasReq ? `slowest 1%: ${fmtMs(rt.p99_ms)}` : undefined} className="bg-surface-2/50" />
        </div>
        {reqs.isError && !reqs.data ? (
          <ErrorState className="mt-4" error={reqs.error} retry={() => reqs.refetch()} />
        ) : (
          <>
            <div className="mt-5 grid gap-5 lg:grid-cols-2">
              <div>
                <h4 className="mb-2 text-[13px] font-semibold text-ink">Requests and failures</h4>
                <MultiLineChart buckets={reqSeries.buckets} series={reqSeries.traffic} bucketSeconds={reqs.data?.bucket_seconds} height={200} loading={reqs.isPending} emptyText="No web requests in this time range" />
              </div>
              <div>
                <h4 className="mb-2 text-[13px] font-semibold text-ink">Response time</h4>
                <MultiLineChart buckets={reqSeries.buckets} series={reqSeries.latency} bucketSeconds={reqs.data?.bucket_seconds} height={200} format={fmtMs} loading={reqs.isPending} emptyText="No timed requests in this time range" />
              </div>
            </div>
            <div className="-mx-4 -mb-4 mt-5 border-t border-border">
              <div className="flex items-center justify-between px-4 pb-1 pt-3">
                <span className="text-[13px] font-semibold text-ink">Busiest endpoints</span>
                <LinkButton to={`/logs?view=analytics&group_by_a=http.route&metric=p95&${rangeQs}`} size="xs" variant="ghost" icon={<ArrowRight size={13} />}>
                  Chart any endpoint
                </LinkButton>
              </div>
              <DataTable
                label="Busiest endpoints"
                columns={routeColumns}
                rows={reqs.data?.routes}
                rowKey={(r) => `${r.method} ${r.route}`}
                rowHref={(r) => `/logs?q=${encodeURIComponent(`http.route:${JSON.stringify(r.route ?? "")} http.method:${r.method ?? ""}`)}&${rangeQs}`}
                loading={reqs.isPending}
                skeletonRows={4}
                stickyHeader={false}
                empty={<EmptyState title="No web requests in this time range" hint="Requests are recorded by the SDK's web integrations and by OpenTelemetry." className="py-8" />}
              />
            </div>
          </>
        )}
      </Section>

      <Section
        title="Messages"
        description="Everything your apps wrote, by severity. Drag across the chart to zoom in."
        actions={
          <LinkButton to={`/logs?${rangeQs}`} size="xs" variant="ghost" icon={<ArrowRight size={13} />}>
            Logs
          </LinkButton>
        }
      >
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
          <StatTile label="Messages logged" value={o ? fmtNumber(o.events) : ""} loading={overview.isPending} error={overview.isError && !o} delta={o && prev ? { current: o.events, previous: prev.events, goodWhen: "neither", label: vs } : undefined} className="bg-surface-2/50" />
          <StatTile label="Apps sending data" value={o ? fmtNumber(o.services) : ""} loading={overview.isPending} error={overview.isError && !o} delta={o && prev ? { current: o.services, previous: prev.services, goodWhen: "neither", label: vs } : undefined} className="bg-surface-2/50" />
          <StatTile label="Share that are errors" value={o && o.events > 0 ? fmtPct((o.errors / o.events) * 100) : "—"} loading={overview.isPending} error={overview.isError && !o} hint={o ? pluralize(o.errors, "error") : undefined} className="bg-surface-2/50" />
        </div>
        <div className="mt-5">{volume.isError && !volume.data ? <ErrorState compact error={volume.error} retry={() => volume.refetch()} title="Couldn't load the chart" /> : <TimelineHistogram data={volume.data} loading={volume.isPending} height={200} onZoom={zoom} />}</div>
      </Section>

      <MetricCard range={range} scope={{ service: scope.service, env: scope.env }} state={{ name: get("m"), agg: get("m_agg"), groupBy: get("m_by") }} onState={(p) => set({ m: p.name, m_agg: p.agg, m_by: p.groupBy })} />
    </Page>
  );
}

const SOURCE_WORDS: Record<"logs" | "otel", { label: string; hint: string }> = {
  logs: { label: "App logs", hint: "Requests recorded by the SDK, and requests reported by OpenTelemetry" },
  otel: { label: "OpenTelemetry metrics", hint: "The http.server.request.duration metric your apps send" },
};

/** "Based on: App logs ▾": where the web-request numbers come from, automatic by default. */
function SourceMenu({ chosen, effective, hasOtel, onChange }: { chosen: Source; effective: "logs" | "otel"; hasOtel: boolean; onChange: (v: Source) => void }) {
  if (!hasOtel) return <span className="text-xs text-ink-3" title={SOURCE_WORDS.logs.hint}>Based on app logs</span>;
  return (
    <Popover
      align="end"
      width={300}
      trigger={({ toggle, open }) => (
        <Button size="xs" variant="ghost" active={open} onClick={toggle} title="Where these numbers come from">
          Based on: {SOURCE_WORDS[effective].label}
          <ChevronDown size={13} className="text-ink-3" />
        </Button>
      )}
    >
      {(close) => (
        <div>
          {(["auto", "logs", "otel"] as Source[]).map((v) => (
            <MenuItem key={v} icon={chosen === v ? <Check size={14} className="text-accent" /> : <span className="inline-block w-[14px]" />} onClick={() => (onChange(v), close())}>
              <span className="min-w-0 flex-1">
                {v === "auto" ? "Automatic" : SOURCE_WORDS[v].label}
                <span className="block text-xs text-ink-3">{v === "auto" ? "App logs, or OpenTelemetry metrics when there are no request events" : SOURCE_WORDS[v].hint}</span>
              </span>
            </MenuItem>
          ))}
        </div>
      )}
    </Popover>
  );
}
