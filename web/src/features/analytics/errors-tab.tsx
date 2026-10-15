import { useMemo } from "react";
import { useErrorAnalytics } from "@/api/hooks";
import type { ErrorGroupCount, PersonImpact, ReleaseHealth } from "@/api/types";
import { MultiLineChart } from "@/components/charts/multi-line";
import { ShareBar } from "@/components/charts/share-bar";
import { SERIES_VARS } from "@/components/charts/palette";
import { DataTable, DeltaText, EmptyState, ErrorState, Section, StatTile, Term, TimeAgo, type Column } from "@/components/ui";
import { fmtHours, fmtNumber, previousLabel, type Range } from "@/lib/time";
import { pluralize } from "@/lib/format";
import { sharePct } from "./chart-model";

export interface AnalyticsScope {
  from: string;
  to: string;
  service: string;
  env: string;
}

export function scopeParams(s: AnalyticsScope) {
  return { from: s.from, to: s.to || undefined, service: s.service || undefined, env: s.env || undefined };
}

/** The range and filters as a query string, for links to Issues and Logs. */
export function scopeQs(s: AnalyticsScope): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(scopeParams(s))) if (v) p.set(k, v);
  return p.toString();
}

/** Errors per 1,000 requests, when the release's requests were counted. */
export function perThousand(r: ReleaseHealth): number | null {
  return r.requests > 0 ? (r.errors / r.requests) * 1000 : null;
}

/** A person as people read them: their e-mail or name when the app sends it, else a short code. */
export function personLabel(p: PersonImpact): string {
  return p.label || `Person ${p.id_hash.slice(0, 6)}`;
}

export function ErrorsTab({ scope, range }: { scope: AnalyticsScope; range: Range }) {
  const q = useErrorAnalytics(scopeParams(scope));
  const d = q.data;
  const vs = previousLabel(range);
  const qs = scopeQs(scope);
  const flow = useMemo(() => {
    const pts = d?.points ?? [];
    return {
      buckets: pts.map((p) => p.ts),
      series: [
        { name: "New problems", values: pts.map((p) => p.opened), color: SERIES_VARS[0] },
        { name: "Fixed", values: pts.map((p) => p.fixed), color: SERIES_VARS[1] },
        { name: "Came back", values: pts.map((p) => p.came_back), color: SERIES_VARS[2] },
      ],
    };
  }, [d]);
  if (q.isError && !d) return <ErrorState error={q.error} retry={() => q.refetch()} />;
  const t = d?.totals;
  const p = d?.previous;
  const ttf = d?.time_to_fix;
  const totalErrors = (d?.types ?? []).reduce((n, g) => n + g.errors, 0);
  const appErrors = (d?.apps ?? []).reduce((n, g) => n + g.errors, 0);
  const loading = q.isPending;

  const groupColumns = (label: string, mono: boolean, total: number, empty: string): Column<ErrorGroupCount>[] => [
    { id: "key", header: label, cell: (g) => <span className={mono ? "truncate font-mono text-[12px] text-ink" : "truncate text-ink"}>{g.key || <em className="font-sans text-ink-3">{empty}</em>}</span> },
    { id: "errors", header: "Errors", width: "84px", align: "right", cell: (g) => <span className="font-medium text-ink">{fmtNumber(g.errors)}</span> },
    { id: "problems", header: <Term k="issue">Problems</Term>, width: "84px", align: "right", hideBelow: "sm", cell: (g) => <span className="text-ink-2">{fmtNumber(g.problems)}</span> },
    { id: "people", header: <Term k="users">People</Term>, width: "72px", align: "right", hideBelow: "md", cell: (g) => <span className="text-ink-2">{fmtNumber(g.people)}</span> },
    { id: "share", header: "Share", width: "160px", hideBelow: "lg", cell: (g) => <ShareBar pct={sharePct(g.errors, total)} /> },
  ];

  const releases = d?.releases ?? [];
  const releaseColumns: Column<ReleaseHealth>[] = [
    { id: "release", header: "Version", cell: (r) => <span className="truncate font-mono text-[12px] text-ink">{r.release}</span> },
    { id: "first", header: "First seen", width: "120px", hideBelow: "md", cell: (r) => <TimeAgo iso={r.first_seen} className="text-ink-2" /> },
    { id: "errors", header: "Errors", width: "80px", align: "right", cell: (r) => <span className="font-medium text-ink">{fmtNumber(r.errors)}</span> },
    {
      id: "rate",
      header: "Per 1,000 requests",
      width: "150px",
      align: "right",
      hideBelow: "sm",
      cell: (r, i) => {
        const rate = perThousand(r);
        if (rate === null) return <span className="text-ink-3">—</span>;
        const older = releases.slice(i + 1).map(perThousand).find((x) => x !== null);
        return (
          <span className="flex flex-col items-end">
            <span className="text-ink">{rate < 10 ? rate.toFixed(2) : rate.toFixed(0)}</span>
            {older != null && older > 0 && <DeltaText current={rate} previous={older} goodWhen="down" />}
          </span>
        );
      },
    },
    { id: "new", header: "New problems", width: "110px", align: "right", hideBelow: "sm", cell: (r) => <span className={r.new_problems > 0 ? "text-ink" : "text-ink-3"}>{fmtNumber(r.new_problems)}</span> },
    { id: "people", header: <Term k="users">People</Term>, width: "72px", align: "right", hideBelow: "md", cell: (r) => <span className="text-ink-2">{fmtNumber(r.people)}</span> },
  ];

  const peopleColumns: Column<PersonImpact>[] = [
    { id: "who", header: "Person", cell: (pi) => <span className={pi.label ? "truncate text-ink" : "truncate font-mono text-[12px] text-ink"}>{personLabel(pi)}</span> },
    { id: "errors", header: "Errors", width: "84px", align: "right", cell: (pi) => <span className="font-medium text-ink">{fmtNumber(pi.errors)}</span> },
    { id: "problems", header: <Term k="issue">Problems</Term>, width: "84px", align: "right", hideBelow: "sm", cell: (pi) => <span className="text-ink-2">{fmtNumber(pi.problems)}</span> },
    { id: "last", header: "Last error", width: "120px", hideBelow: "md", cell: (pi) => <TimeAgo iso={pi.last_seen} className="text-ink-2" /> },
  ];

  const spread = ttf
    ? [
        { label: "Within an hour", n: ttf.within_hour },
        { label: "Within a day", n: ttf.within_day },
        { label: "Within a week", n: ttf.within_week },
        { label: "Longer", n: ttf.longer },
      ]
    : [];

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile label="New problems" value={t ? fmtNumber(t.opened) : ""} loading={loading} delta={t && p ? { current: t.opened, previous: p.opened, goodWhen: "down", label: vs } : undefined} />
        <StatTile label="Fixed" value={t ? fmtNumber(t.fixed) : ""} loading={loading} delta={t && p ? { current: t.fixed, previous: p.fixed, goodWhen: "up", label: vs } : undefined} />
        <StatTile label={<Term k="regressed">Came back</Term>} value={t ? fmtNumber(t.came_back) : ""} loading={loading} hint="fixed, then seen again" delta={t && p ? { current: t.came_back, previous: p.came_back, goodWhen: "down", label: vs } : undefined} />
        <StatTile label="Typical time to fix" value={ttf && ttf.fixed > 0 ? fmtHours(ttf.median_hours) : "—"} loading={loading} hint={ttf ? (ttf.fixed > 1 ? `half of ${pluralize(ttf.fixed, "fix", "fixes")} took less` : ttf.fixed === 1 ? "the one fix in this range" : "nothing fixed in this range") : undefined} />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Section title="Opened and fixed" description="Problems that appeared, were marked fixed, and came back after a fix." className="lg:col-span-2">
          <MultiLineChart buckets={flow.buckets} series={flow.series} bucketSeconds={d?.bucket_seconds} height={220} loading={loading} curve="linear" emptyText="Nothing opened or fixed in this time range" />
        </Section>
        <Section title="Time to fix" description="How long the problems fixed in this range had been open, counted from when they appeared or last came back.">
          {loading ? (
            <div className="skeleton h-32" />
          ) : ttf && ttf.fixed > 0 ? (
            <ul className="space-y-3">
              {spread.map((s) => (
                <li key={s.label} className="text-[13px]">
                  <div className="mb-1 flex justify-between gap-2">
                    <span className="text-ink">{s.label}</span>
                    <span className="tabular text-ink-2">{fmtNumber(s.n)}</span>
                  </div>
                  <ShareBar pct={sharePct(s.n, ttf.fixed)} />
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-[13px] text-ink-3">No problem was marked fixed in this time range.</p>
          )}
        </Section>
      </div>

      <Section title="By version" description="Did a release make things worse? Errors next to the requests each version answered, when your app counts them." flush>
        <DataTable
          label="Errors by version"
          columns={releaseColumns}
          rows={d ? releases : undefined}
          rowKey={(r) => r.release}
          rowHref={(r) => `/issues?release=${encodeURIComponent(r.release)}&${qs}`}
          loading={loading}
          skeletonRows={4}
          stickyHeader={false}
          empty={<EmptyState title="No versions yet" hint="Set release when you set up the Python package, and each version shows up here." className="py-8" />}
        />
      </Section>

      <div className="grid gap-4 xl:grid-cols-2">
        <Section title="Most frequent error types" flush>
          <DataTable
            label="Most frequent error types"
            columns={groupColumns("Error type", true, totalErrors, "Logged errors without an exception")}
            rows={d?.types}
            rowKey={(g) => g.key || "__none"}
            rowHref={(g) => (g.key ? `/issues?q=${encodeURIComponent(g.key)}&${qs}` : `/issues?${qs}`)}
            loading={loading}
            skeletonRows={5}
            stickyHeader={false}
            empty={<EmptyState title="No errors in this time range" className="py-8" />}
          />
        </Section>
        <Section title="Noisiest apps" flush>
          <DataTable
            label="Noisiest apps"
            columns={groupColumns("App", false, appErrors, "No app name")}
            rows={d?.apps}
            rowKey={(g) => g.key || "__none"}
            rowHref={(g) => `/issues?service=${encodeURIComponent(g.key)}&${qs}`}
            loading={loading}
            skeletonRows={5}
            stickyHeader={false}
            empty={<EmptyState title="No errors in this time range" className="py-8" />}
          />
        </Section>
      </div>

      <Section
        title="Most affected people"
        description={
          <>
            Signed-in people who hit the most errors. Names show only when your app sends them; otherwise each person is a short code.
            {d ? ` Error details are kept for ${pluralize(d.details_kept_days, "day")}, so this looks back no further.` : ""}
          </>
        }
        flush
      >
        <DataTable
          label="Most affected people"
          columns={peopleColumns}
          rows={d?.people}
          rowKey={(pi) => pi.id_hash}
          rowHref={(pi) => `/logs?q=${encodeURIComponent(`user:${pi.id_hash}`)}&${qs}`}
          loading={loading}
          skeletonRows={4}
          stickyHeader={false}
          empty={<EmptyState title="No signed-in people hit an error" hint="Tell Pyloggle who is signed in (set_user) and the people errors reach show up here." className="py-8" />}
        />
      </Section>
    </div>
  );
}
