import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import clsx from "clsx";
import { get } from "@/api/client";
import type { Me, TimeSeriesPoint } from "@/api/types";
import { TrendChart } from "@/components/charts/trend-chart";
import { Card, DataTable, EmptyState, Page, QueryState, Section, Skeleton, StatTile, TimeAgo, buttonClass, type Column } from "@/components/ui";
import { fmtBytes, fmtNumber, fmtPct, fmtWhen } from "@/lib/time";
import { alertKindLabel } from "@/features/alerts/alert-words";
import { LearnMore } from "@/components/learn-more";

interface SystemInfo {
  version: string;
  go_version: string;
  started_at: string;
  uptime_s: number;
  clickhouse: {
    up: boolean;
    version: string;
    sampled_at: string;
    error?: string;
    disk: { free: number; total: number };
    mutations_pending: number;
    tables: { name: string; rows: number; bytes: number; uncompressed: number; parts: number; oldest_partition?: string; retention_days?: number }[];
  };
  ingest: {
    rate: TimeSeriesPoint[];
    events: Record<string, number>;
    rejected: Record<string, number>;
    requests: Record<string, number>;
    queues: { kind: string; depth: number; capacity: number; written: number; dropped: number; flush_failures: number }[];
    streams: { active: number; overflow: number };
    spans: Record<string, number>;
    points: Record<string, number>;
  };
  retention: Record<string, number>;
  alerting: { enabled: boolean; interval_s: number; rules_enabled: number; last_run?: string | null; evaluations: Record<string, number>; firings: Record<string, number>; notify_failures: Record<string, number> };
}

const OUTCOME_LABEL: Record<string, string> = {
  accepted: "Saved",
  quota: "Dropped: project over its storage limit",
  rate_limit: "Dropped: too many per second",
  sampled: "Dropped on purpose (sampling)",
  queue_full: "Dropped: server too busy",
};

const KIND_LABEL: Record<string, string> = { logs: "Messages", errors: "Error details", spans: "Request steps", metrics: "Custom measurements" };

/** Storage tables in words; the real table name stays visible underneath. */
const TABLE_LABEL: Record<string, string> = {
  logs_raw: "Messages",
  error_occurrences: "Error details",
  spans: "Request steps",
  metric_points: "Custom measurements",
  metrics_catalog: "Measurement names",
  issues_agg: "Issue summaries",
  issues_state: "Issue status",
  issues_activity: "Issue activity",
  issues_stats: "Issue statistics (old)",
  issue_counts_hourly: "Errors per hour",
  log_counts_minutely: "Messages per minute",
  request_stats_minutely: "Requests per minute",
  releases: "Versions",
  alert_rules: "Alert rules",
  alert_firings: "Alerts sent",
  saved_searches: "Saved searches",
  projects: "Projects",
  project_api_keys: "Access keys",
  project_api_key_usage: "Access key use",
  project_usage_daily: "Storage per day",
  admin_audit: "Admin activity",
  pyloggle_settings: "Server settings",
  schema_migrations: "Database upgrades",
};

function counts(m: Record<string, number>, label: (k: string) => string = (k) => k): string {
  const e = Object.entries(m);
  return e.length ? e.map(([k, v]) => `${label(k)} ${fmtNumber(v)}`).join(" · ") : "none";
}

function fmtUptime(s: number): string {
  if (s < 60) return "less than a minute";
  if (s < 120) return "1 minute";
  if (s < 3600) return `${Math.floor(s / 60)} minutes`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min`;
  const d = Math.floor(s / 86400);
  return `${d} ${d === 1 ? "day" : "days"} ${Math.floor((s % 86400) / 3600)} h`;
}

/** Operator view in plain words: the numbers /metrics exposes, refreshed every 15 seconds. */
export function SystemPage({ me }: { me: Me }) {
  const q = useQuery({ queryKey: ["system"], queryFn: () => get<SystemInfo>("/system"), refetchInterval: 15_000, enabled: me.is_admin });
  const d = q.data;
  const totalEvents = d ? Object.values(d.ingest.events).reduce((n, v) => n + v, 0) : 0;
  const dropped = d ? totalEvents - (d.ingest.events.accepted ?? 0) : 0;
  const diskPct = d && d.clickhouse.disk.total > 0 ? ((d.clickhouse.disk.total - d.clickhouse.disk.free) / d.clickhouse.disk.total) * 100 : 0;
  const notifyFailures = d ? Object.values(d.alerting.notify_failures).reduce((n, v) => n + v, 0) : 0;
  return (
    <Page
      title="System"
      description={
        <>
          {d ? `Health of the Pyloggle server itself, for administrators. Version ${d.version}, running for ${fmtUptime(d.uptime_s)}.` : "Health of the Pyloggle server itself, for administrators."}{" "}
          <LearnMore doc="retention">How much it keeps</LearnMore>
        </>
      }
      actions={
        me.is_admin && (
          <a href="/metrics" target="_blank" rel="noreferrer" className={buttonClass({ className: "gap-1.5" })}>
            <ExternalLink size={14} /> Raw metrics (Prometheus)
          </a>
        )
      }
    >
      {!me.is_admin ? (
        <Card>
          <EmptyState title="For administrators only" hint="Ask an administrator if you need to see the server's health." />
        </Card>
      ) : (
        <QueryState query={q} skeleton={<Skeleton rows={8} />}>
          {(d) => (
            <>
              <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
                <StatTile label="Database" value={d.clickhouse.up ? "Running" : "Down"} hint={d.clickhouse.up ? `ClickHouse ${d.clickhouse.version}` : d.clickhouse.error || "Not checked yet"} tone={d.clickhouse.up ? "good" : "critical"} />
                <StatTile label="Disk space used" value={d.clickhouse.disk.total ? fmtPct(diskPct) : "—"} hint={d.clickhouse.disk.total ? `${fmtBytes(d.clickhouse.disk.free)} free of ${fmtBytes(d.clickhouse.disk.total)}` : "Unknown"} tone={diskPct >= 90 ? "critical" : diskPct >= 75 ? "warning" : undefined} />
                <StatTile label="Received" value={fmtNumber(totalEvents)} hint="since the server started" />
                <StatTile label="Dropped" value={fmtNumber(dropped)} hint={totalEvents ? `${fmtPct((dropped / totalEvents) * 100)} of what arrived` : "Nothing yet"} tone={dropped > 0 ? "warning" : undefined} />
                <StatTile label="Cleanup tasks running" value={fmtNumber(d.clickhouse.mutations_pending)} hint="deletions and trims in progress" tone={d.clickhouse.mutations_pending > 5 ? "warning" : undefined} />
                <StatTile label="Open connections" value={fmtNumber(d.ingest.streams.active)} hint={d.ingest.streams.overflow ? `${fmtNumber(d.ingest.streams.overflow)} turned away` : "apps sending right now"} tone={d.ingest.streams.overflow ? "warning" : undefined} />
              </div>
              <div className="grid gap-5 lg:grid-cols-2">
                <Section title="Incoming data" description="Messages received per minute, all projects, last hour.">
                  <TrendChart points={d.ingest.rate} height={180} color="var(--accent)" label="messages" />
                </Section>
                <Section title="Waiting to be saved" description="What is queued for the database, by kind. Full queues drop data.">
                  <div className="space-y-4 text-[13px]">
                    {d.ingest.queues.map((qu) => {
                      const pct = qu.capacity ? (qu.depth / qu.capacity) * 100 : 0;
                      return (
                        <div key={qu.kind}>
                          <div className="flex flex-wrap items-baseline justify-between gap-2">
                            <span className="font-medium text-ink">{KIND_LABEL[qu.kind] ?? qu.kind}</span>
                            <span className="text-xs tabular text-ink-3">
                              {fmtNumber(qu.depth)} waiting of {fmtNumber(qu.capacity)} · {fmtNumber(qu.written)} saved
                              {qu.dropped > 0 && <span className="text-warning-text"> · {fmtNumber(qu.dropped)} dropped</span>}
                              {qu.flush_failures > 0 && <span className="text-critical-text"> · {fmtNumber(qu.flush_failures)} failed saves</span>}
                            </span>
                          </div>
                          <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-surface-3">
                            <div className={clsx("h-1.5 rounded-full", pct >= 80 ? "bg-critical" : "bg-accent")} style={{ width: `${Math.max(pct > 0 ? 1 : 0, Math.min(100, pct))}%` }} />
                          </div>
                        </div>
                      );
                    })}
                    {d.ingest.queues.length === 0 && <div className="text-ink-3">No queues reported.</div>}
                  </div>
                </Section>
              </div>
              <div className="grid gap-5 lg:grid-cols-2">
                <Section title="What happened to incoming data" description="Since the server started." flush>
                  <ul className="divide-y divide-border text-[13px]">
                    {Object.entries(d.ingest.events)
                      .sort((a, b) => b[1] - a[1])
                      .map(([k, v]) => (
                        <li key={k} className="flex items-center gap-3 px-4 py-2">
                          <span className={clsx("flex-1", k === "accepted" ? "text-ink" : "text-warning-text")}>{OUTCOME_LABEL[k] ?? k}</span>
                          <span className="w-16 text-right text-xs tabular text-ink-3">{totalEvents ? fmtPct((v / totalEvents) * 100) : ""}</span>
                          <span className="w-20 text-right tabular text-ink">{fmtNumber(v)}</span>
                        </li>
                      ))}
                    {Object.entries(d.ingest.rejected).map(([k, v]) => (
                      <li key={"rej-" + k} className="flex items-center gap-3 px-4 py-2">
                        <span className="flex-1 text-critical-text">Couldn't be read ({k})</span>
                        <span className="w-20 text-right tabular text-ink">{fmtNumber(v)}</span>
                      </li>
                    ))}
                    {Object.keys(d.ingest.events).length === 0 && <li className="px-4 py-3 text-ink-3">Nothing received since the server started.</li>}
                  </ul>
                </Section>
                <Section title="Alerts engine" description={d.alerting.enabled ? `Checks the rules every ${d.alerting.interval_s} seconds.` : "Switched off (setting ALERTING_ENABLED)."}>
                  <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-[13px]">
                    <dt className="text-ink-3">Rules turned on</dt>
                    <dd className="text-ink">{d.alerting.rules_enabled}</dd>
                    <dt className="text-ink-3">Last check</dt>
                    <dd className="text-ink">{d.alerting.last_run ? <TimeAgo iso={d.alerting.last_run} /> : "Never"}</dd>
                    <dt className="text-ink-3">Checks done</dt>
                    <dd className="text-ink">{counts(d.alerting.evaluations, alertKindLabel)}</dd>
                    <dt className="text-ink-3">Alerts sent</dt>
                    <dd className="text-ink">{counts(d.alerting.firings, alertKindLabel)}</dd>
                    <dt className="text-ink-3">Failed to send</dt>
                    <dd className={notifyFailures ? "text-critical-text" : "text-ink"}>{counts(d.alerting.notify_failures)}</dd>
                    <dt className="text-ink-3">Data is kept for</dt>
                    <dd className="text-ink">
                      messages {d.retention.logs} days · error details {d.retention.errors} days · request steps {d.retention.spans} days · measurements {d.retention.metrics} days · statistics {d.retention.stats} days
                    </dd>
                  </dl>
                </Section>
              </div>
              <Section title="Storage by table" description={d.clickhouse.sampled_at ? `Checked ${fmtWhen(d.clickhouse.sampled_at)}.` : "Not checked yet."} flush>
                <DataTable
                  label="Storage by table"
                  columns={tableColumns}
                  rows={[...d.clickhouse.tables].sort((a, b) => b.bytes - a.bytes)}
                  rowKey={(t) => t.name}
                  stickyHeader={false}
                  empty={<EmptyState title="No table statistics yet" hint={d.clickhouse.error || "The server checks the database once a minute."} className="py-6" />}
                />
              </Section>
            </>
          )}
        </QueryState>
      )}
    </Page>
  );
}

type Table = SystemInfo["clickhouse"]["tables"][number];

const tableColumns: Column<Table>[] = [
  {
    id: "name",
    header: "What it holds",
    cell: (t) => (
      <div className="min-w-0">
        <div className="truncate text-ink">{TABLE_LABEL[t.name] ?? t.name}</div>
        <div className="truncate font-mono text-[11px] text-ink-3">{t.name}</div>
        {/* On a phone, how long it is kept goes under its name. */}
        {t.retention_days ? <div className="truncate text-xs text-ink-3 sm:hidden">kept for {t.retention_days} days</div> : null}
      </div>
    ),
  },
  { id: "rows", header: "Rows", width: "100px", align: "right", hideBelow: "md", cell: (t) => <span className="text-ink-2">{fmtNumber(t.rows)}</span> },
  { id: "bytes", header: "On disk", width: "100px", align: "right", cell: (t) => <span className="font-medium text-ink">{fmtBytes(t.bytes)}</span> },
  { id: "ratio", header: "Compression", width: "110px", align: "right", hideBelow: "lg", cell: (t) => <span className="text-ink-3">{t.uncompressed ? `${(t.uncompressed / Math.max(1, t.bytes)).toFixed(1)}× smaller` : "—"}</span> },
  { id: "oldest", header: "Oldest data", width: "120px", hideBelow: "md", cell: (t) => <span className="font-mono text-[12px] text-ink-3">{t.oldest_partition || "—"}</span> },
  { id: "retention", header: "Kept for", width: "90px", align: "right", hideBelow: "sm", cell: (t) => <span className="text-ink-3">{t.retention_days ? `${t.retention_days} days` : "—"}</span> },
];
