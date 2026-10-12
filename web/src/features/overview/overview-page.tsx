import type { ReactNode } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { AlertTriangle, ArrowRight, Bell, CheckCircle2, CircleDashed, OctagonAlert, Package, Rocket } from "lucide-react";
import { useAlertFirings, useAttention, useErrorTrend, useOverview, useReleases, useRequestStats, useServiceHealth } from "@/api/hooks";
import type { AttentionItem, HealthStatus, Me, ServiceHealth } from "@/api/types";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { MiniTrend, Sparkline } from "@/components/charts/sparkline";
import { Badge, DataTable, EmptyState, ErrorState, LinkButton, MetaLine, Page, QueryState, Section, Skeleton, StatTile, Term, TimeAgo, type BadgeTone, type Column } from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { fmtMs, fmtNumber, fmtPct, previousLabel, rangePhrase, type Range } from "@/lib/time";
import { pluralize } from "@/lib/format";
import { alertKindLabel, firingRuleName } from "@/features/alerts/alert-words";

const OVERVIEW_DEFAULT: Range = { from: "-24h", to: "" };

/**
 * The home page: "Is everything OK right now?" A status line, the headline numbers against the
 * previous period, what needs attention, how each app is doing, recent versions and alerts.
 */
export function OverviewPage({ me }: { me: Me }) {
  const { range, setRange, isLive } = useRange(OVERVIEW_DEFAULT);
  const scope = { from: range.from, to: range.to || undefined };
  const live = isLive ? 60_000 : false;
  const overview = useOverview({ ...scope, compare: "previous" }, live);
  const requests = useRequestStats({ ...scope, compare: "previous", buckets: 48 });
  const errorTrend = useErrorTrend({ ...scope, buckets: 48 });
  const attention = useAttention({ ...scope, limit: 6 }, live);
  const apps = useServiceHealth(scope, live);
  const releases = useReleases({ ...scope, limit: 5 });
  const alerting = !!me.features.alerting;
  const alerts = useAlertFirings(5, alerting);
  const refetchAll = () => [overview, requests, errorTrend, attention, apps, releases, alerts].forEach((q) => q.refetch());
  const fetching = [overview, requests, attention, apps].some((q) => q.isFetching);
  const vs = previousLabel(range);
  const issuesLink = (extra: Record<string, string>) => `/issues?${new URLSearchParams({ from: range.from, ...(range.to ? { to: range.to } : {}), ...extra })}`;

  const o = overview.data?.overview;
  const prev = overview.data?.previous;
  const totals = requests.data?.totals;
  const prevTotals = requests.data?.previous_totals;
  const hasRequests = !!totals && totals.requests > 0;

  return (
    <Page
      title="Overview"
      description={`Is everything OK right now? A summary of ${rangePhrase(range)}.`}
      actions={<TimeRangePicker value={range} onChange={setRange} onRefresh={refetchAll} refreshing={fetching} />}
    >
      <StatusBanner attention={attention} apps={apps} overview={overview} issuesLink={issuesLink} />

      {/* Five tiles: one row on wide screens, otherwise three then two wider ones (no holes). */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-6 xl:grid-cols-5 [&>*]:lg:col-span-2 [&>*]:xl:col-span-1 [&>*:nth-child(n+4)]:lg:col-span-3 [&>*:nth-child(n+4)]:xl:col-span-1 [&>*:last-child]:max-lg:col-span-2">
        <StatTile
          label="Errors"
          value={o ? fmtNumber(o.errors) : ""}
          loading={overview.isPending}
          error={overview.isError && !o}
          delta={o && prev ? { current: o.errors, previous: prev.errors, goodWhen: "down", label: vs } : undefined}
          chart={errorTrend.data && errorTrend.data.points.some((p) => p.count > 0) ? <MiniTrend values={errorTrend.data.points.map((p) => p.count)} color="var(--lvl-error)" title="Errors over time" /> : undefined}
        />
        <StatTile
          label={<Term k="users">People affected</Term>}
          value={o ? fmtNumber(o.affected_users) : ""}
          loading={overview.isPending}
          error={overview.isError && !o}
          delta={o && prev ? { current: o.affected_users, previous: prev.affected_users, goodWhen: "down", label: vs } : undefined}
          hint={o && !prev ? "signed-in people who hit an error" : undefined}
        />
        <StatTile
          label={<Term k="failed">Failed requests</Term>}
          value={hasRequests ? fmtPct(totals.error_pct) : "—"}
          tone={hasRequests ? (totals.error_pct >= 5 ? "critical" : totals.error_pct >= 1 ? "warning" : undefined) : undefined}
          loading={requests.isPending}
          error={requests.isError && !totals}
          delta={hasRequests && prevTotals && prevTotals.requests > 0 ? { current: totals.error_pct, previous: prevTotals.error_pct, goodWhen: "down", label: vs } : undefined}
          hint={hasRequests ? `${fmtNumber(totals.errors)} of ${fmtNumber(totals.requests)} requests` : "No web requests recorded"}
          chart={hasRequests && requests.data ? <MiniTrend values={requests.data.points.map((p) => p.requests)} title="Requests over time" /> : undefined}
        />
        <StatTile
          label={<Term k="p50">Typical response time</Term>}
          value={hasRequests ? fmtMs(totals.p50_ms) : "—"}
          loading={requests.isPending}
          error={requests.isError && !totals}
          delta={hasRequests && prevTotals && prevTotals.requests > 0 ? { current: totals.p50_ms, previous: prevTotals.p50_ms, goodWhen: "down", label: vs } : undefined}
          hint={hasRequests ? `Slowest 5%: ${fmtMs(totals.p95_ms)}` : undefined}
        />
        <StatTile
          label="Messages logged"
          value={o ? fmtNumber(o.events) : ""}
          loading={overview.isPending}
          error={overview.isError && !o}
          delta={o && prev ? { current: o.events, previous: prev.events, goodWhen: "neither", label: vs } : undefined}
          hint={o ? `from ${pluralize(o.services, "app")}` : undefined}
        />
      </div>

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <Section
          id="attention"
          title="Needs attention"
          description="Problems that are new, came back after a fix, or suddenly happen more often."
          actions={<LinkButton to={issuesLink({})} size="xs" variant="ghost" icon={<ArrowRight size={13} />}>All issues</LinkButton>}
          flush
        >
          <QueryState
            query={attention}
            isEmpty={(d) => d.items.length === 0}
            skeleton={<Skeleton rows={5} className="p-4" />}
            empty={<EmptyState icon={<CheckCircle2 size={18} className="text-good-text" />} title="Nothing needs attention" hint={`No new problems, nothing came back, and nothing is spiking in ${rangePhrase(range)}.`} />}
          >
            {(d) => (
              <ul className="divide-y divide-border">
                {d.items.map((it) => (
                  <AttentionRow key={it.issue.issue_id} item={it} />
                ))}
              </ul>
            )}
          </QueryState>
        </Section>

        <div className="flex min-w-0 flex-col gap-5">
          <Section title="Recent versions" description="When each version was first seen, and its errors in this range." flush>
            <QueryState
              query={releases}
              compact
              isEmpty={(d) => d.items.length === 0}
              skeleton={<Skeleton rows={3} className="p-4" />}
              empty={<EmptyState icon={<Package size={18} />} title="No versions reported" hint="Set release in the SDK (or service.version in OpenTelemetry) to follow each deploy here." className="py-8" />}
            >
              {(d) => (
                <ul className="divide-y divide-border">
                  {d.items.map((r) => (
                    <li key={r.release}>
                      <Link to={issuesLink({ status: "all", release: r.release })} className="flex items-center gap-3 px-4 py-2.5 hover:bg-surface-2">
                        <Rocket size={14} className="flex-none text-ink-3" />
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-[13px] font-medium text-ink">{r.release}</div>
                          <div className="text-xs text-ink-3">
                            first seen <TimeAgo iso={r.first_seen} />
                          </div>
                        </div>
                        <span className={clsx("flex-none text-xs tabular", r.errors > 0 ? "font-medium text-critical-text" : "text-ink-3")}>{r.errors > 0 ? pluralize(r.errors, "error") : "No errors"}</span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </QueryState>
          </Section>

          {alerting && (
            <Section title="Recent alerts" description="The last notifications Pyloggle sent." actions={<LinkButton to="/alerts" size="xs" variant="ghost" icon={<ArrowRight size={13} />}>Alerts</LinkButton>} flush>
              <QueryState
                query={alerts}
                compact
                isEmpty={(d) => d.items.length === 0}
                skeleton={<Skeleton rows={3} className="p-4" />}
                empty={<EmptyState icon={<Bell size={18} />} title="No alerts sent yet" hint="When an alert rule fires, it shows up here." className="py-8" />}
              >
                {(d) => (
                  <ul className="divide-y divide-border">
                    {d.items.map((f) => (
                      <li key={f.id} className="flex items-start gap-3 px-4 py-2.5">
                        <Bell size={14} className="mt-0.5 flex-none text-ink-3" />
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-[13px] font-medium text-ink">{firingRuleName(f)}</div>
                          <MetaLine>
                            {alertKindLabel(f.condition_type)}
                            {f.service}
                            <TimeAgo iso={f.fired_at} />
                          </MetaLine>
                        </div>
                      </li>
                    ))}
                  </ul>
                )}
              </QueryState>
            </Section>
          )}
        </div>
      </div>

      <Section id="apps" title="Apps" description="How each app is doing: traffic, failures, speed and errors." flush>
        <AppsTable apps={apps} range={range} />
      </Section>
    </Page>
  );
}

type Q<T> = { data?: T; isPending: boolean; isError: boolean; error: unknown; refetch: () => unknown; fetchStatus: "fetching" | "paused" | "idle" };

/** One line that answers the page's question, with each reason linking to where to look. */
function StatusBanner({ attention, apps, overview, issuesLink }: { attention: Q<{ counts: Record<string, number> }>; apps: Q<{ items: ServiceHealth[] }>; overview: Q<{ overview: { events: number } }>; issuesLink: (extra: Record<string, string>) => string }) {
  if (attention.isPending || apps.isPending || overview.isPending) {
    return <div className="skeleton h-[60px] rounded-lg" aria-label="Loading status" />;
  }
  if (attention.isError && !attention.data) {
    return (
      <div className="rounded-lg border border-border bg-surface">
        <ErrorState compact error={attention.error} retry={() => attention.refetch()} title="Couldn't work out the current status" />
      </div>
    );
  }
  const counts = attention.data?.counts ?? {};
  const failing = (apps.data?.items ?? []).filter((a) => a.status === "failing");
  const noData = (overview.data?.overview.events ?? 0) === 0 && (apps.data?.items.length ?? 0) === 0;
  const reasons: { text: string; to: string }[] = [];
  for (const a of failing) reasons.push({ text: `${a.service} is failing: ${fmtPct(a.failed_pct)} of requests failed`, to: `/logs?q=${encodeURIComponent(`service:${a.service} level:error`)}` });
  if (counts.regressed) reasons.push({ text: `${pluralize(counts.regressed, "problem")} came back after being fixed`, to: issuesLink({ status: "regressed" }) });
  if (counts.new) reasons.push({ text: `${pluralize(counts.new, "new problem")}`, to: issuesLink({ sort: "first_seen" }) });
  if (counts.spike) reasons.push({ text: `${pluralize(counts.spike, "problem")} happening much more often than before`, to: "#attention" });
  const silent = (apps.data?.items ?? []).filter((a) => a.status === "silent");
  if (silent.length === 1) reasons.push({ text: `${silent[0].service || "An app"} stopped sending data`, to: "#apps" });
  else if (silent.length > 1) reasons.push({ text: `${silent.length} apps stopped sending data`, to: "#apps" });

  let tone: "good" | "warning" | "critical" | "neutral" = "good";
  let icon: ReactNode = <CheckCircle2 size={20} />;
  let title = "All clear";
  let sub: ReactNode = "No new problems, nothing came back after a fix, and no app is failing.";
  if (noData) {
    tone = "neutral";
    icon = <CircleDashed size={20} />;
    title = "No data in this time range";
    sub = (
      <>
        Nothing was received. If you're just starting,{" "}
        <Link to="/projects" className="font-medium text-accent hover:underline">
          connect your app
        </Link>
        .
      </>
    );
  } else if (reasons.length > 0) {
    tone = failing.length > 0 ? "critical" : "warning";
    icon = failing.length > 0 ? <OctagonAlert size={20} /> : <AlertTriangle size={20} />;
    title = reasons.length === 1 ? "1 thing needs attention" : `${reasons.length} things need attention`;
    sub = (
      <ul className="flex flex-wrap gap-x-4 gap-y-1">
        {reasons.map((r) => (
          <li key={r.text}>
            {r.to.startsWith("#") ? (
              <a href={r.to} className="hover:text-ink hover:underline">
                {r.text}
              </a>
            ) : (
              <Link to={r.to} className="hover:text-ink hover:underline">
                {r.text}
              </Link>
            )}
          </li>
        ))}
      </ul>
    );
  }
  return (
    <div
      role="status"
      className={clsx(
        "flex items-start gap-3 rounded-lg border px-4 py-3",
        tone === "good" && "border-good/25 bg-good/[0.06]",
        tone === "warning" && "border-warning/35 bg-warning/[0.08]",
        tone === "critical" && "border-critical/30 bg-critical/[0.06]",
        tone === "neutral" && "border-border bg-surface",
      )}
    >
      <span className={clsx("mt-0.5 flex-none", tone === "good" && "text-good-text", tone === "warning" && "text-warning-text", tone === "critical" && "text-critical-text", tone === "neutral" && "text-ink-3")}>{icon}</span>
      <div className="min-w-0">
        <div className="text-[15px] font-semibold text-ink">{title}</div>
        <div className="mt-0.5 text-[13px] text-ink-2">{sub}</div>
      </div>
    </div>
  );
}

function reasonBadge(item: AttentionItem, reason: string) {
  if (reason === "regressed") return <Badge key={reason} tone="critical" title="Marked as fixed, then happened again (regressed)">Came back</Badge>;
  if (reason === "new") return <Badge key={reason} tone="accent" title="First seen in this time range">New</Badge>;
  const ratio = item.previous_count > 0 ? item.count / item.previous_count : 0;
  const text = ratio > 0 ? `${ratio >= 10 ? Math.round(ratio) : ratio.toFixed(1)}× more often` : "Suddenly frequent";
  return (
    <Badge key={reason} tone="warning" title={`${fmtNumber(item.count)} times now, ${fmtNumber(item.previous_count)} in the period before`}>
      {text}
    </Badge>
  );
}

function AttentionRow({ item }: { item: AttentionItem }) {
  const s = item.issue;
  const [headline, ...rest] = (s.title || s.last_message || "Unknown error").split(": ");
  const message = rest.join(": ") || (s.last_message && s.last_message !== s.title ? s.last_message : "");
  return (
    <li>
      <Link to={`/issues/${s.issue_id}`} className="flex items-start gap-4 px-4 py-3 hover:bg-surface-2">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="truncate text-[13px] font-semibold text-ink">{headline}</span>
            {item.reasons.map((r) => reasonBadge(item, r))}
          </div>
          {message && <div className="mt-0.5 truncate text-[13px] text-ink-2">{message}</div>}
          <MetaLine className="mt-1">
            {s.service}
            {s.env}
            {s.culprit && <span className="font-mono text-[11px]">{s.culprit}</span>}
            <span>
              last seen <TimeAgo iso={s.last_seen} />
            </span>
          </MetaLine>
        </div>
        <div className="flex flex-none flex-col items-end gap-1 pt-0.5">
          <Sparkline values={s.sparkline} width={96} height={20} title="Times per hour, last 24 hours" />
          <span className="text-xs text-ink-3 tabular">{pluralize(item.count, "time")}</span>
        </div>
      </Link>
    </li>
  );
}

const HEALTH: Record<HealthStatus, { label: string; tone: BadgeTone; explain: string }> = {
  failing: { label: "Failing", tone: "critical", explain: "5% or more of its requests failed" },
  degraded: { label: "Needs a look", tone: "warning", explain: "Some requests failed or errors were reported" },
  silent: { label: "No recent data", tone: "neutral", explain: "Nothing received in the last hour of this range, so its current state is unknown" },
  ok: { label: "OK", tone: "good", explain: "Requests are succeeding and no errors were reported" },
};

function AppsTable({ apps, range }: { apps: Q<{ items: ServiceHealth[] }>; range: Range }) {
  const rangeQs = `from=${encodeURIComponent(range.from)}${range.to ? `&to=${encodeURIComponent(range.to)}` : ""}`;
  const columns: Column<ServiceHealth>[] = [
    {
      id: "app",
      header: <Term k="service">App</Term>,
      width: "minmax(160px,1.6fr)",
      cell: (a) => (
        <div className="min-w-0">
          <div className="truncate font-medium text-ink">{a.service || "(unnamed app)"}</div>
          {a.envs.length > 0 && <div className="truncate text-xs text-ink-3">{a.envs.join(", ")}</div>}
        </div>
      ),
    },
    {
      id: "status",
      header: "Status",
      width: "130px",
      cell: (a) => (
        <Badge tone={HEALTH[a.status].tone} title={HEALTH[a.status].explain}>
          {HEALTH[a.status].label}
        </Badge>
      ),
    },
    { id: "rpm", header: <Term k="requestsPerMin">Requests / min</Term>, width: "120px", align: "right", cell: (a) => (a.requests > 0 ? fmtRate(a.rpm) : <span className="text-ink-3">—</span>) },
    {
      id: "failed",
      header: <Term k="failed">Failed</Term>,
      width: "90px",
      align: "right",
      cell: (a) =>
        a.requests > 0 ? <span className={clsx(a.status !== "silent" && (a.failed_pct >= 5 ? "font-medium text-critical-text" : a.failed_pct >= 1 && "text-warning-text"))}>{fmtPct(a.failed_pct)}</span> : <span className="text-ink-3">—</span>,
    },
    { id: "p50", header: <Term k="p50">Typical time</Term>, width: "110px", align: "right", cell: (a) => (a.requests > 0 ? fmtMs(a.p50_ms) : <span className="text-ink-3">—</span>) },
    { id: "errors", header: "Errors", width: "80px", align: "right", cell: (a) => (a.errors > 0 ? fmtNumber(a.errors) : <span className="text-ink-3">0</span>) },
    { id: "logs", header: "Messages", width: "90px", align: "right", cell: (a) => fmtNumber(a.logs) },
    { id: "seen", header: "Last seen", width: "110px", align: "right", cell: (a) => (a.last_seen && !a.last_seen.startsWith("1970") ? <TimeAgo iso={a.last_seen} className="text-ink-2" /> : <span className="text-ink-3">—</span>) },
    {
      id: "go",
      header: <span className="sr-only">Open</span>,
      width: "72px",
      align: "right",
      cell: (a) => (
        <LinkButton to={`/issues?${rangeQs}&service=${encodeURIComponent(a.service)}`} size="xs" variant="ghost">
          Issues
        </LinkButton>
      ),
    },
  ];
  return (
    <DataTable
      label="Apps"
      columns={columns}
      rows={apps.data?.items}
      rowKey={(a) => a.service}
      rowHref={(a) => `/logs?${rangeQs}&q=${encodeURIComponent(`service:${a.service}`)}`}
      loading={apps.isPending}
      error={apps.isError ? apps.error : undefined}
      onRetry={() => apps.refetch()}
      empty={<EmptyState title="No apps have sent data in this range" hint="Apps appear here as soon as they send logs, errors or requests." />}
      stickyHeader={false}
    />
  );
}

function fmtRate(perMin: number): string {
  if (perMin >= 100) return fmtNumber(Math.round(perMin));
  if (perMin >= 0.1) return perMin.toFixed(1);
  if (perMin > 0) return "<0.1";
  return "0";
}
