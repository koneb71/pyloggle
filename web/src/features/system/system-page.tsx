import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import clsx from "clsx";
import { get } from "@/api/client";
import type { Me, TimeSeriesPoint } from "@/api/types";
import { PageHeader } from "@/components/shell/app-shell";
import { TrendChart } from "@/components/charts/trend-chart";
import { Card, CardHeader, EmptyState, ErrorState, Skeleton, StatTile, TimeAgo } from "@/components/ui";
import { fmtBytes, fmtNumber, fmtPct, fmtTs } from "@/lib/time";

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

const OUTCOME_LABEL: Record<string, string> = { accepted: "accepted", quota: "dropped: storage quota", rate_limit: "dropped: rate limit", sampled: "dropped: sampling", queue_full: "dropped: queue full" };

function fmtUptime(s: number): string {
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
}

/** Operator view: the same numbers /metrics exposes, refreshed every 15 seconds. */
export function SystemPage({ me }: { me: Me }) {
  const q = useQuery({ queryKey: ["system"], queryFn: () => get<SystemInfo>("/system"), refetchInterval: 15_000, enabled: me.is_admin });
  if (!me.is_admin) return <EmptyState title="Admins only" hint="The System page is limited to PYLOGGLE_UI_ADMINS." />;
  const d = q.data;
  const totalEvents = d ? Object.values(d.ingest.events).reduce((n, v) => n + v, 0) : 0;
  const dropped = d ? totalEvents - (d.ingest.events.accepted ?? 0) : 0;
  const diskPct = d && d.clickhouse.disk.total > 0 ? ((d.clickhouse.disk.total - d.clickhouse.disk.free) / d.clickhouse.disk.total) * 100 : 0;
  return (
    <>
      <PageHeader title="System" sub={d ? `ingestd ${d.version} · ${d.go_version} · up ${fmtUptime(d.uptime_s)}` : "ingestd health"}>
        <a href="/metrics" target="_blank" rel="noreferrer" className="inline-flex h-7 items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 text-xs font-medium text-ink hover:bg-surface-2">
          <ExternalLink size={12} /> /metrics
        </a>
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[1400px] flex-col gap-3 p-4">
          {q.isError && <ErrorState error={q.error} retry={() => q.refetch()} />}
          {q.isPending ? (
            <Skeleton rows={8} className="p-3" />
          ) : d ? (
            <>
              <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
                <StatTile label="ClickHouse" value={d.clickhouse.up ? "up" : "down"} hint={d.clickhouse.up ? d.clickhouse.version : d.clickhouse.error || "no sample yet"} tone={d.clickhouse.up ? "good" : "critical"} />
                <StatTile label="Disk used" value={d.clickhouse.disk.total ? fmtPct(diskPct) : "–"} hint={d.clickhouse.disk.total ? `${fmtBytes(d.clickhouse.disk.free)} free of ${fmtBytes(d.clickhouse.disk.total)}` : "default disk unknown"} tone={diskPct >= 90 ? "critical" : diskPct >= 75 ? "warning" : undefined} />
                <StatTile label="Events processed" value={fmtNumber(totalEvents)} hint="since start, every door" />
                <StatTile label="Dropped" value={fmtNumber(dropped)} hint={totalEvents ? `${fmtPct((dropped / totalEvents) * 100)} of events` : "nothing yet"} tone={dropped > 0 ? "warning" : undefined} />
                <StatTile label="Mutations pending" value={fmtNumber(d.clickhouse.mutations_pending)} hint="deletes and trims in flight" tone={d.clickhouse.mutations_pending > 5 ? "warning" : undefined} />
                <StatTile label="Ingest streams" value={fmtNumber(d.ingest.streams.active)} hint={d.ingest.streams.overflow ? `${fmtNumber(d.ingest.streams.overflow)} overflowed` : "no overflow"} tone={d.ingest.streams.overflow ? "warning" : undefined} />
              </div>
              <div className="grid gap-3 lg:grid-cols-2">
                <Card>
                  <CardHeader title="Ingest rate" sub="log lines per minute, every project, last hour" />
                  <div className="p-2">
                    <TrendChart points={d.ingest.rate} height={180} color="var(--accent)" label="lines" />
                  </div>
                </Card>
                <Card>
                  <CardHeader title="Write queues" sub="rows waiting for ClickHouse, per kind" />
                  <div className="space-y-2 p-3 text-xs">
                    {d.ingest.queues.map((qu) => {
                      const pct = qu.capacity ? (qu.depth / qu.capacity) * 100 : 0;
                      return (
                        <div key={qu.kind}>
                          <div className="flex items-baseline justify-between">
                            <span className="font-medium text-ink">{qu.kind}</span>
                            <span className="tabular text-ink-3">
                              {fmtNumber(qu.depth)} / {fmtNumber(qu.capacity)} · {fmtNumber(qu.written)} written · {fmtNumber(qu.dropped)} dropped · {fmtNumber(qu.flush_failures)} failed flushes
                            </span>
                          </div>
                          <div className="mt-1 h-1.5 w-full overflow-hidden rounded bg-surface-3">
                            <div className={clsx("h-1.5 rounded", pct >= 80 ? "bg-critical" : "bg-accent")} style={{ width: `${Math.max(pct > 0 ? 1 : 0, Math.min(100, pct))}%` }} />
                          </div>
                        </div>
                      );
                    })}
                    {d.ingest.queues.length === 0 && <div className="text-ink-3">no queues reported</div>}
                  </div>
                </Card>
              </div>
              <div className="grid gap-3 lg:grid-cols-2">
                <Card>
                  <CardHeader title="Events by outcome" sub="since start" />
                  <table className="w-full text-xs">
                    <tbody>
                      {Object.entries(d.ingest.events)
                        .sort((a, b) => b[1] - a[1])
                        .map(([k, v]) => (
                          <tr key={k} className="border-t border-border">
                            <td className={clsx("px-3 py-1.5", k === "accepted" ? "text-ink" : "text-serious")}>{OUTCOME_LABEL[k] ?? k}</td>
                            <td className="px-3 py-1.5 text-right tabular text-ink">{fmtNumber(v)}</td>
                            <td className="px-3 py-1.5 text-right tabular text-ink-3">{totalEvents ? fmtPct((v / totalEvents) * 100) : ""}</td>
                          </tr>
                        ))}
                      {Object.entries(d.ingest.rejected).map(([k, v]) => (
                        <tr key={"rej-" + k} className="border-t border-border">
                          <td className="px-3 py-1.5 text-critical">undecodable ({k})</td>
                          <td className="px-3 py-1.5 text-right tabular text-ink">{fmtNumber(v)}</td>
                          <td />
                        </tr>
                      ))}
                      {Object.keys(d.ingest.events).length === 0 && (
                        <tr>
                          <td className="px-3 py-2 text-ink-3" colSpan={3}>
                            nothing ingested since start
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </Card>
                <Card>
                  <CardHeader title="Alerting" sub={d.alerting.enabled ? `every ${d.alerting.interval_s}s · ${d.alerting.rules_enabled} rules enabled` : "disabled (ALERTING_ENABLED=false)"} />
                  <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 px-3 py-2 text-xs">
                    <dt className="text-ink-3">last run</dt>
                    <dd className="text-ink">{d.alerting.last_run ? <TimeAgo iso={d.alerting.last_run} withAbsolute /> : "never"}</dd>
                    <dt className="text-ink-3">evaluations</dt>
                    <dd className="tabular text-ink">{Object.entries(d.alerting.evaluations).map(([k, v]) => `${k} ${fmtNumber(v)}`).join(" · ") || "0"}</dd>
                    <dt className="text-ink-3">firings</dt>
                    <dd className="tabular text-ink">{Object.entries(d.alerting.firings).map(([k, v]) => `${k} ${fmtNumber(v)}`).join(" · ") || "0"}</dd>
                    <dt className="text-ink-3">notify failures</dt>
                    <dd className={clsx("tabular", Object.keys(d.alerting.notify_failures).length ? "text-critical" : "text-ink")}>{Object.entries(d.alerting.notify_failures).map(([k, v]) => `${k} ${fmtNumber(v)}`).join(" · ") || "0"}</dd>
                    <dt className="text-ink-3">retention</dt>
                    <dd className="text-ink">
                      logs {d.retention.logs}d · errors {d.retention.errors}d · spans {d.retention.spans}d · metrics {d.retention.metrics}d · stats {d.retention.stats}d
                    </dd>
                  </dl>
                </Card>
              </div>
              <Card>
                <CardHeader title="Tables" sub={d.clickhouse.sampled_at ? `active parts, sampled ${fmtTs(d.clickhouse.sampled_at)}` : "no sample yet"} />
                {d.clickhouse.tables.length === 0 ? (
                  <EmptyState title="No table statistics" hint={d.clickhouse.error || "The collector samples ClickHouse once a minute."} className="py-6" />
                ) : (
                  <table className="w-full text-xs">
                    <thead className="text-left text-[10px] uppercase tracking-wide text-ink-3">
                      <tr className="border-b border-border">
                        <th className="px-3 py-1.5 font-medium">table</th>
                        <th className="px-3 py-1.5 text-right font-medium">rows</th>
                        <th className="px-3 py-1.5 text-right font-medium">on disk</th>
                        <th className="px-3 py-1.5 text-right font-medium">uncompressed</th>
                        <th className="px-3 py-1.5 text-right font-medium">ratio</th>
                        <th className="px-3 py-1.5 text-right font-medium">parts</th>
                        <th className="px-3 py-1.5 font-medium">oldest partition</th>
                        <th className="px-3 py-1.5 text-right font-medium">retention</th>
                      </tr>
                    </thead>
                    <tbody>
                      {[...d.clickhouse.tables]
                        .sort((a, b) => b.bytes - a.bytes)
                        .map((t) => (
                          <tr key={t.name} className="border-b border-border hover:bg-surface-2">
                            <td className="px-3 py-1 font-mono text-ink">{t.name}</td>
                            <td className="px-3 py-1 text-right tabular text-ink-2">{fmtNumber(t.rows)}</td>
                            <td className="px-3 py-1 text-right tabular text-ink">{fmtBytes(t.bytes)}</td>
                            <td className="px-3 py-1 text-right tabular text-ink-2">{fmtBytes(t.uncompressed)}</td>
                            <td className="px-3 py-1 text-right tabular text-ink-3">{t.uncompressed ? `${(t.uncompressed / Math.max(1, t.bytes)).toFixed(1)}×` : ""}</td>
                            <td className="px-3 py-1 text-right tabular text-ink-2">{t.parts}</td>
                            <td className="px-3 py-1 font-mono text-ink-3">{t.oldest_partition}</td>
                            <td className="px-3 py-1 text-right tabular text-ink-3">{t.retention_days ? `${t.retention_days}d` : ""}</td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                )}
              </Card>
            </>
          ) : null}
        </div>
      </div>
    </>
  );
}
