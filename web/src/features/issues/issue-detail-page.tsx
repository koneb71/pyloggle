import { useCallback } from "react";
import { Link, useNavigate, useParams } from "react-router";
import clsx from "clsx";
import { ChevronLeft, ChevronRight, GitBranch, History } from "lucide-react";
import { useIssue, useOccurrence, useOccurrences } from "@/api/hooks";
import type { FacetCount, IssueDetail, IssueSummary, Occurrence } from "@/api/types";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { TrendChart } from "@/components/charts/trend-chart";
import { Badge, Button, Card, CopyButton, EmptyState, ErrorState, Page, QueryState, Section, Skeleton, StatusBadge, Tabs, Term, TimeAgo, UnhandledBadge } from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { durationWords, fmtNumber, fmtTs, fmtWhen, rangePhrase } from "@/lib/time";
import { levelVar } from "@/lib/levels";
import { pluralize } from "@/lib/format";
import { sourceLabel } from "@/lib/fields";
import { issueHeadline } from "./issue-text";
import { ActivityTab, AssigneePicker, StateQualifiers, WorkflowActions } from "./workflow";
import { OCCURRENCE_TABS, OccurrenceTab } from "./occurrence-tabs";

const ISSUE_DEFAULT = { from: "-7d", to: "" };

export function IssueDetailPage() {
  const { id } = useParams();
  const { range, setRange } = useRange(ISSUE_DEFAULT);
  const { get, set } = useUrlState();
  const detail = useIssue(id, { from: range.from, to: range.to });
  const eventId = get("event");
  const occQ = useOccurrence(eventId || undefined);
  const occ: Occurrence | undefined = eventId ? occQ.data : detail.data?.latest;
  const tab = get("tab", "exception");
  const nav = useNavigate();
  const issue = detail.data?.issue;
  const { headline, message } = issue ? issueHeadline(issue) : { headline: "", message: "" };
  const rangeQs = `from=${encodeURIComponent(range.from)}${range.to ? `&to=${encodeURIComponent(range.to)}` : ""}`;
  const onFilterLogs = useCallback((q: string) => nav(`/logs?q=${encodeURIComponent(q)}&${rangeQs}`), [nav, rangeQs]);

  return (
    <Page
      back={{ to: `/issues?${rangeQs}`, label: "Issues" }}
      title={issue ? headline : detail.isError ? "Issue" : <span className="skeleton inline-block h-6 w-72 align-middle" aria-label="Loading" />}
      description={
        issue && (
          <span className="block">
            {message && <span className="block whitespace-pre-wrap break-words text-[14px] text-ink-2">{message}</span>}
            {issue.culprit && (
              <span className="mt-1 block text-[13px] text-ink-3">
                <Term k="culprit">Happened in</Term> <span className="font-mono text-[12px] text-ink-2">{issue.culprit}</span>
              </span>
            )}
          </span>
        )
      }
      actions={
        <>
          {issue && <WorkflowActions issue={issue} />}
          <TimeRangePicker value={range} onChange={setRange} onRefresh={() => detail.refetch()} refreshing={detail.isFetching} />
        </>
      }
    >
      {detail.isError ? (
        <Card>
          <ErrorState error={detail.error} retry={() => detail.refetch()} />
        </Card>
      ) : !issue ? (
        <Skeleton rows={8} />
      ) : (
        <>
          <SummaryBand d={detail.data!} />
          <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
            <div className="flex min-w-0 flex-col gap-5">
              <HowOften d={detail.data!} rangeText={rangePhrase(range)} />
              <Card className="min-w-0">
                <OccurrenceNav occ={occ} issueId={id!} isLatest={!eventId} onSelect={(ev) => set({ event: ev || undefined })} />
                <Tabs
                  className="px-4"
                  value={tab}
                  onChange={(t) => set({ tab: t === "exception" ? undefined : t }, true)}
                  items={OCCURRENCE_TABS.map((t) => ({ id: t.id, label: t.label, count: t.id === "breadcrumbs" ? occ?.breadcrumbs?.length : undefined }))}
                />
                <div className="p-4">
                  {tab === "activity" ? (
                    <ActivityTab issueId={id!} />
                  ) : occ ? (
                    <OccurrenceTab tab={tab} occ={occ} onFilterLogs={onFilterLogs} />
                  ) : eventId && occQ.isError ? (
                    <ErrorState error={occQ.error} retry={() => occQ.refetch()} />
                  ) : eventId && occQ.isPending ? (
                    <Skeleton rows={6} />
                  ) : (
                    <NoDetails />
                  )}
                </div>
              </Card>
            </div>
            <div className="flex min-w-0 flex-col gap-5">
              <Section title="This time" description={occ ? undefined : "No details recorded"} flush>
                {occ ? <ThisTime occ={occ} /> : <p className="px-4 py-3 text-[13px] text-ink-3">The details of past times were cleaned up by the storage limits.</p>}
              </Section>
              <WhereItHappens d={detail.data!} />
              <RecentTimes issueId={id!} current={occ?.event_id} range={range} onSelect={(ev) => set({ event: ev })} />
            </div>
          </div>
        </>
      )}
    </Page>
  );
}

function NoDetails() {
  return <EmptyState icon={<History size={18} />} title="No details recorded" hint="The details of each time are kept for a limited period. The counts above are kept longer." className="py-8" />;
}

/** The facts about the issue as a whole, each with a label. */
function SummaryBand({ d }: { d: IssueDetail }) {
  const it = d.issue;
  const crashed = d.breakdowns.handled?.some((h) => h.value === "false" || h.value === "unhandled");
  const fields: [React.ReactNode, React.ReactNode][] = [
    [
      "Status",
      <span key="s" className="flex flex-wrap items-center gap-1.5">
        <StatusBadge status={it.status} regressed={it.is_regressed} />
        {crashed && <UnhandledBadge />}
        <StateQualifiers issue={it} />
      </span>,
    ],
    [<Term key="t" k="events">Times</Term>, <span key="v" className="tabular">{fmtNumber(it.occurrence_count)}</span>],
    [<Term key="p" k="users">People affected</Term>, <span key="v" className="tabular">{fmtNumber(it.affected_users)}</span>],
    ["First seen", <TimeAgo key="f" iso={it.first_seen} />],
    ["Last seen", <TimeAgo key="l" iso={it.last_seen} />],
    [<Term key="r" k="release">Version</Term>, it.last_release || <span className="text-ink-3">Not reported</span>],
    [<Term key="a" k="service">App</Term>, [it.service, it.env].filter(Boolean).join(" · ")],
    ["Assigned to", <AssignedTo key="as" issue={it} />],
  ];
  return (
    <Card className="grid grid-cols-2 gap-x-6 gap-y-4 px-5 py-4 sm:grid-cols-4 xl:grid-cols-8">
      {fields.map(([label, value], i) => (
        <div key={i} className={clsx("min-w-0", i === 0 && "col-span-2 sm:col-span-1")}>
          <div className="text-xs text-ink-3">{label}</div>
          <div className="mt-1 truncate text-[14px] font-medium text-ink">{value}</div>
        </div>
      ))}
    </Card>
  );
}

function AssignedTo({ issue }: { issue: IssueSummary }) {
  return (
    <span className="flex min-w-0 items-center gap-1">
      <span className={clsx("truncate", !issue.assignee && "text-ink-3")}>{issue.assignee || "No one"}</span>
      <AssigneePicker issue={issue} compact />
    </span>
  );
}

function HowOften({ d, rangeText }: { d: IssueDetail; rangeText: string }) {
  const pts = d.trend ?? [];
  const total = pts.reduce((n, p) => n + p.count, 0);
  const step = pts.length > 1 ? (new Date(pts[1].ts).getTime() - new Date(pts[0].ts).getTime()) / 1000 : 0;
  return (
    <Section title="How often it happened" description={`${pluralize(total, "time")} in ${rangeText}${step ? ` · each point is ${durationWords(step)}` : ""}`}>
      <TrendChart points={pts} color={levelVar(d.issue.level)} label="times" height={150} variant="bars" />
    </Section>
  );
}

function OccurrenceNav({ occ, issueId, isLatest, onSelect }: { occ?: Occurrence; issueId: string; isLatest: boolean; onSelect: (ev: string | null) => void }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-2.5">
      <div className="flex min-w-0 items-center gap-2 text-[13px]">
        <span className="font-semibold text-ink">{isLatest ? "The latest time" : "One time it happened"}</span>
        {occ && (
          <>
            <span className="text-ink-3 tabular" title={fmtTs(occ.ts, { ms: true })}>
              {fmtWhen(occ.ts, { seconds: true })}
            </span>
            <CopyButton text={`${location.origin}/ui/issues/${issueId}?event=${occ.event_id}`} label="Copy a link to this time" />
          </>
        )}
      </div>
      <div className="flex items-center gap-1">
        <Button size="xs" icon={<ChevronLeft size={13} />} disabled={!occ?.prev_event_id} onClick={() => onSelect(occ!.prev_event_id!)} title="The time before this one">
          Earlier
        </Button>
        <Button size="xs" disabled={!occ?.next_event_id} onClick={() => onSelect(occ!.next_event_id!)} title="The time after this one">
          Later
          <ChevronRight size={13} />
        </Button>
        {!isLatest && (
          <Button size="xs" variant="ghost" onClick={() => onSelect(null)}>
            Latest
          </Button>
        )}
      </div>
    </div>
  );
}

/** Facts about the time shown, with plain labels; ids stay copyable. */
function ThisTime({ occ }: { occ: Occurrence }) {
  const rows: [string, React.ReactNode, string?][] = [
    ["When", <span key="w" title={fmtTs(occ.ts, { ms: true })}>{fmtWhen(occ.ts, { seconds: true })}</span>],
    ["App", occ.service],
    ["Environment", occ.env],
    ["Version", occ.release],
    ["Server", occ.hostname],
    ["Code module", occ.logger && <span key="l" className="font-mono text-[12px]">{occ.logger}</span>],
    ["Request ID", occ.request_id && <span key="r" className="font-mono text-[12px]">{occ.request_id}</span>, occ.request_id],
    ["Sent by", occ.sdk ? `${occ.sdk.name} ${occ.sdk.version}` : sourceLabel(occ.source)],
  ];
  return (
    <div>
      <dl className="divide-y divide-border">
        {rows
          .filter(([, v]) => v)
          .map(([k, v, copy]) => (
            <div key={k} className="group flex items-center gap-3 px-4 py-2 text-[13px]">
              <dt className="w-24 flex-none text-ink-3">{k}</dt>
              <dd className="flex min-w-0 flex-1 items-center gap-1 text-ink">
                <span className="min-w-0 truncate">{v}</span>
                {copy && <CopyButton text={copy} label={`Copy ${k}`} className="invisible pointer-coarse:visible group-hover:visible" />}
              </dd>
            </div>
          ))}
      </dl>
      {occ.trace_id && (
        <div className="border-t border-border px-4 py-2.5">
          <Link to={`/traces/${occ.trace_id}?at=${encodeURIComponent(occ.ts)}`} className="inline-flex items-center gap-1.5 text-[13px] font-medium text-accent hover:underline">
            <GitBranch size={14} /> See the request timeline
          </Link>
        </div>
      )}
    </div>
  );
}

const BREAKDOWN_LABEL: Record<string, string> = { env: "Environment", release: "Version", http_route: "Route", hostname: "Server", user: "Person" };

/** Where the issue happens: fields with one value become one line, the rest get bars. */
function WhereItHappens({ d }: { d: IssueDetail }) {
  const entries = Object.entries(d.breakdowns)
    .filter(([k]) => k !== "handled" && BREAKDOWN_LABEL[k])
    .map(([k, items]) => [k, items.filter((i) => i.value !== "")] as const)
    .filter(([, items]) => items.length > 0);
  if (entries.length === 0) return null;
  const single = entries.filter(([k, items]) => items.length === 1 && k !== "user");
  const multi = entries.filter(([k, items]) => items.length > 1 && k !== "user");
  const people = d.breakdowns.user?.filter((u) => u.value !== "").length ?? 0;
  return (
    <Section title="Where it happens" description="Across every time in this range." flush>
      {single.length > 0 && (
        <dl className="space-y-1 border-b border-border px-4 py-3 text-[13px] last:border-b-0">
          {single.map(([k, items]) => (
            <div key={k} className="flex gap-3">
              <dt className="w-24 flex-none text-ink-3">{BREAKDOWN_LABEL[k]}</dt>
              <dd className="min-w-0 truncate text-ink" title={items[0].value}>
                <span className="text-ink-3">Always</span> <span className={clsx(k === "http_route" && "font-mono text-[12px]")}>{items[0].value}</span>
              </dd>
            </div>
          ))}
        </dl>
      )}
      {multi.map(([k, items]) => (
        <BreakdownBars key={k} field={k} label={BREAKDOWN_LABEL[k]} items={items} />
      ))}
      {people > 0 && <p className="px-4 py-3 text-xs text-ink-3">{people >= 6 ? "Spread across many people." : `${pluralize(people, "person", "people")} hit this most.`}</p>}
    </Section>
  );
}

function BreakdownBars({ field, label, items }: { field: string; label: string; items: FacetCount[] }) {
  const total = items.reduce((n, i) => n + i.count, 0) || 1;
  return (
    <div className="border-b border-border px-4 py-3 last:border-b-0">
      <div className="mb-2 text-xs font-medium text-ink-2">{label}</div>
      <ul className="space-y-2">
        {items.map((i) => (
          <li key={i.value} className="text-[13px]">
            <div className="flex items-center justify-between gap-2">
              <span className={clsx("min-w-0 truncate text-ink", field === "http_route" && "font-mono text-[12px]")} title={i.value}>
                {i.value}
              </span>
              <span className="flex-none text-xs tabular text-ink-3" title={i.users ? `${pluralize(i.users, "person", "people")} affected` : undefined}>
                {Math.round((i.count / total) * 100)}%
              </span>
            </div>
            <div className="mt-1 h-1.5 w-full rounded-full bg-surface-3">
              <div className="h-1.5 rounded-full bg-accent" style={{ width: `${Math.max(2, (i.count / total) * 100)}%` }} />
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function RecentTimes({ issueId, current, range, onSelect }: { issueId: string; current?: string; range: { from: string; to: string }; onSelect: (ev: string) => void }) {
  const q = useOccurrences(issueId, 30, { from: range.from, to: range.to || undefined });
  return (
    <Section title="Recent times" description="Pick one to see its details." flush>
      <QueryState query={q} compact isEmpty={(d) => d.items.length === 0} skeleton={<Skeleton rows={5} className="p-4" />} empty={<p className="px-4 py-3 text-[13px] text-ink-3">No details recorded in this time range.</p>}>
        {(d) => (
          <ul className="max-h-96 divide-y divide-border overflow-y-auto">
            {d.items.map((o) => (
              <li key={o.event_id}>
                <button
                  type="button"
                  onClick={() => onSelect(o.event_id)}
                  aria-current={o.event_id === current || undefined}
                  className={clsx("flex w-full items-center gap-3 px-4 py-2 text-left text-[13px] hover:bg-surface-2", o.event_id === current && "bg-accent-soft hover:bg-accent-soft")}
                >
                  <span className="flex-none tabular text-ink">{fmtWhen(o.ts, { seconds: true })}</span>
                  <span className="min-w-0 flex-1 truncate text-right text-xs text-ink-3">{o.http_route ? `${o.http_method} ${o.http_route}` : o.hostname || o.env}</span>
                  {o.http_status >= 500 ? <Badge tone="critical">{o.http_status}</Badge> : null}
                </button>
              </li>
            ))}
          </ul>
        )}
      </QueryState>
    </Section>
  );
}
