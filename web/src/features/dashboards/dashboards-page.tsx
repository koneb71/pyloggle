import { useMemo } from "react";
import { Link } from "react-router";
import { Users } from "lucide-react";
import { useErrorTrend, useLogVolume, useOverview, useTopIssues } from "@/api/hooks";
import { PageHeader } from "@/components/shell/app-shell";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { TimelineHistogram } from "@/components/charts/timeline-histogram";
import { TrendChart } from "@/components/charts/trend-chart";
import { Sparkline } from "@/components/charts/sparkline";
import { Badge, Card, CardHeader, EmptyState, ErrorState, LevelBadge, Skeleton, StatTile, StatusBadge, TimeAgo } from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { bucketLabel, fmtNumber } from "@/lib/time";
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
  const o = overview.data?.overview;
  const errRate = o && o.events > 0 ? (o.errors / o.events) * 100 : 0;

  return (
    <>
      <PageHeader title="Dashboards" sub="Project health at a glance">
        <TimeRangePicker value={range} onChange={setRange} onRefresh={() => (overview.refetch(), errors.refetch(), volume.refetch(), top.refetch())} refreshing={overview.isFetching} />
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
