import { useMemo } from "react";
import { useUsageAnalytics } from "@/api/hooks";
import type { EndpointUsage, VersionUsage } from "@/api/types";
import { MultiLineChart } from "@/components/charts/multi-line";
import { ShareBar } from "@/components/charts/share-bar";
import { SERIES_VARS } from "@/components/charts/palette";
import { DataTable, EmptyState, ErrorState, SegmentedControl, Section, StatTile, Term, TimeAgo, type Column } from "@/components/ui";
import { LearnMore } from "@/components/learn-more";
import { useUrlState } from "@/lib/url";
import { fmtNumber, fmtPct, previousLabel, type Range } from "@/lib/time";
import { chartHref, DEFAULT_CHART, sharePct } from "./chart-model";
import { scopeParams, type AnalyticsScope } from "./errors-tab";

const failedPct = (u: { requests: number; errors: number }) => (u.requests > 0 ? (u.errors / u.requests) * 100 : 0);

function FailedCell({ u }: { u: { requests: number; errors: number } }) {
  const pct = failedPct(u);
  return <span className={pct >= 5 ? "font-medium text-critical-text" : pct >= 1 ? "text-warning-text" : "text-ink-3"}>{fmtPct(pct)}</span>;
}

/** The chart of one endpoint's requests in Analytics → Charts. */
export function endpointChartHref(e: EndpointUsage, scope: AnalyticsScope): string {
  return chartHref({ ...DEFAULT_CHART.requests, q: `endpoint:${JSON.stringify(`${e.method} ${e.route}`)}`, groupBy: "" }, { from: scope.from, to: scope.to }, scope);
}

export function UsageTab({ scope, range }: { scope: AnalyticsScope; range: Range }) {
  const { get, set } = useUrlState();
  const sort = get("by") === "people" ? "people" : "requests";
  const per = get("per") === "week" ? "week" : "bucket";
  const q = useUsageAnalytics({ ...scopeParams(scope), sort });
  const d = q.data;
  const vs = previousLabel(range);
  const bucketWord = !d ? "day" : d.bucket_seconds <= 3600 ? "hour" : d.bucket_seconds <= 86400 ? "day" : "week";
  const people = useMemo(() => {
    const pts = (per === "week" ? d?.weeks : d?.points) ?? [];
    return { buckets: pts.map((p) => p.ts), series: [{ name: "People", values: pts.map((p) => p.people), color: SERIES_VARS[0] }], seconds: per === "week" ? 7 * 86400 : d?.bucket_seconds };
  }, [d, per]);
  const traffic = useMemo(() => {
    const pts = d?.points ?? [];
    return {
      buckets: pts.map((p) => p.ts),
      series: [
        { name: "Requests", values: pts.map((p) => p.requests), color: SERIES_VARS[0] },
        { name: "Failed requests", values: pts.map((p) => p.errors), color: "var(--lvl-error)" },
      ],
    };
  }, [d]);
  const versions = useMemo(() => (d?.version_series ?? []).map((s, i) => ({ name: s.name === "" ? "No version" : s.name === "other" ? "Other" : s.name, values: s.values, color: s.name === "other" ? "var(--lvl-other)" : SERIES_VARS[i % SERIES_VARS.length] })), [d]);

  if (q.isError && !d) return <ErrorState error={q.error} retry={() => q.refetch()} />;
  const t = d?.totals;
  const p = d?.previous;
  const loading = q.isPending;
  if (d && t && t.requests === 0 && p && p.requests === 0) {
    return (
      <Section title="No requests counted yet">
        <EmptyState
          title="Requests are counted as soon as your app answers them"
          hint={
            <>
              Your app needs this version of the Python package with its Flask, FastAPI or Django integration, or to send OpenTelemetry. Requests are counted without appearing on Logs. <LearnMore doc="sdkConfiguration" anchor="request-counts">How requests are counted</LearnMore>
            </>
          }
          className="py-8"
        />
      </Section>
    );
  }

  const endpointColumns: Column<EndpointUsage>[] = [
    {
      id: "endpoint",
      header: "Page or endpoint",
      cell: (e) => (
        <span className="truncate font-mono text-[12px] text-ink" title={`${e.method} ${e.route}`}>
          <span className="text-ink-3">{e.method}</span> {e.route}
        </span>
      ),
    },
    { id: "requests", header: "Requests", width: "96px", align: "right", cell: (e) => <span className={sort === "requests" ? "font-medium text-ink" : "text-ink-2"}>{fmtNumber(e.requests)}</span> },
    { id: "people", header: <Term k="people">People</Term>, width: "80px", align: "right", cell: (e) => <span className={sort === "people" ? "font-medium text-ink" : "text-ink-2"}>{fmtNumber(e.people)}</span> },
    { id: "failed", header: <Term k="failed">Failed</Term>, width: "72px", align: "right", hideBelow: "sm", cell: (e) => <FailedCell u={e} /> },
    { id: "share", header: "Share of requests", width: "170px", hideBelow: "lg", cell: (e) => <ShareBar pct={sharePct(e.requests, t?.requests ?? 0)} /> },
  ];
  const versionColumns: Column<VersionUsage>[] = [
    { id: "release", header: "Version", cell: (v) => (v.release ? <span className="truncate font-mono text-[12px] text-ink">{v.release}</span> : <em className="text-ink-3">No version</em>) },
    { id: "first", header: "First seen", width: "120px", hideBelow: "md", cell: (v) => (v.first_seen ? <TimeAgo iso={v.first_seen} className="text-ink-2" /> : <span className="text-ink-3">—</span>) },
    { id: "requests", header: "Requests", width: "96px", align: "right", cell: (v) => <span className="font-medium text-ink">{fmtNumber(v.requests)}</span> },
    { id: "people", header: <Term k="people">People</Term>, width: "80px", align: "right", hideBelow: "sm", cell: (v) => <span className="text-ink-2">{fmtNumber(v.people)}</span> },
    { id: "failed", header: <Term k="failed">Failed</Term>, width: "72px", align: "right", cell: (v) => <FailedCell u={v} /> },
  ];

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile label="Requests" value={t ? fmtNumber(t.requests) : ""} loading={loading} delta={t && p ? { current: t.requests, previous: p.requests, goodWhen: "neither", label: vs } : undefined} />
        <StatTile label={<Term k="people">People</Term>} value={t ? fmtNumber(t.people) : ""} loading={loading} hint="signed in" delta={t && p ? { current: t.people, previous: p.people, goodWhen: "up", label: vs } : undefined} />
        <StatTile label="Requests per person" value={t && t.people > 0 ? fmtNumber(Math.round(t.requests / t.people)) : "—"} loading={loading} hint={t && t.people === 0 ? "nobody signed in" : undefined} />
        <StatTile
          label={<Term k="failed">Failed requests</Term>}
          value={t && t.requests > 0 ? fmtPct(failedPct(t)) : "—"}
          tone={t && t.requests > 0 ? (failedPct(t) >= 5 ? "critical" : failedPct(t) >= 1 ? "warning" : undefined) : undefined}
          loading={loading}
          hint={t && t.requests > 0 ? `${fmtNumber(t.errors)} of ${fmtNumber(t.requests)}` : undefined}
          delta={t && p && t.requests > 0 && p.requests > 0 ? { current: failedPct(t), previous: failedPct(p), goodWhen: "down", label: vs } : undefined}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Section
          title="Active people"
          description={t && t.people === 0 ? "Nobody signed in made a request. People are counted when your app tells Pyloggle who is signed in (set_user)." : `Different signed-in people each ${per === "week" ? "week" : bucketWord}.`}
          actions={
            <SegmentedControl
              label="People per"
              value={per}
              onChange={(v) => set({ per: v === "bucket" ? undefined : v }, true)}
              options={[
                { value: "bucket", label: `By ${bucketWord}` },
                { value: "week", label: "By week" },
              ]}
            />
          }
        >
          <MultiLineChart buckets={people.buckets} series={people.series} bucketSeconds={people.seconds} height={200} loading={loading} hideLegend emptyText="No signed-in people in this time range" />
        </Section>
        <Section title="Requests and failures" description={`Requests answered each ${bucketWord}, and how many failed.`}>
          <MultiLineChart buckets={traffic.buckets} series={traffic.series} bucketSeconds={d?.bucket_seconds} height={200} loading={loading} emptyText="No requests in this time range" />
        </Section>
      </div>

      <Section
        title="Pages and endpoints"
        description="The most used ones, by requests or by how many people use them. Open one to chart it."
        actions={
          <SegmentedControl
            label="Rank by"
            value={sort}
            onChange={(v) => set({ by: v === "requests" ? undefined : v }, true)}
            options={[
              { value: "requests", label: "Requests" },
              { value: "people", label: "People" },
            ]}
          />
        }
        flush
      >
        <DataTable
          label="Pages and endpoints"
          columns={endpointColumns}
          rows={d?.endpoints}
          rowKey={(e) => `${e.method} ${e.route}`}
          rowHref={(e) => endpointChartHref(e, scope)}
          loading={loading}
          skeletonRows={6}
          stickyHeader={false}
          empty={<EmptyState title="No requests in this time range" className="py-8" />}
        />
      </Section>

      <Section title="Traffic by version" description="Requests each version of your apps answered: watch a release take over." flush>
        <div className="border-b border-border p-4">
          <MultiLineChart buckets={(d?.points ?? []).map((pt) => pt.ts)} series={versions} bucketSeconds={d?.bucket_seconds} height={200} loading={loading} emptyText="No requests in this time range" />
        </div>
        <DataTable
          label="Traffic by version"
          columns={versionColumns}
          rows={d?.versions}
          rowKey={(v) => v.release || "__none"}
          loading={loading}
          skeletonRows={3}
          stickyHeader={false}
          empty={<EmptyState title="No requests in this time range" className="py-8" />}
        />
      </Section>
    </div>
  );
}
