import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import clsx from "clsx";
import {
  ArrowLeft,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  GitBranch,
  Users,
} from "lucide-react";
import {
  useIssue,
  useIssueTrend,
  useOccurrence,
  useOccurrences,
} from "@/api/hooks";
import type { FacetCount, IssueDetail, Occurrence } from "@/api/types";
import { PageHeader } from "@/components/shell/app-shell";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { TrendChart } from "@/components/charts/trend-chart";
import {
  Badge,
  Button,
  Card,
  CardHeader,
  CopyButton,
  EmptyState,
  ErrorState,
  JsonView,
  KeyValueList,
  LevelBadge,
  Skeleton,
  StatusBadge,
  Tabs,
  TimeAgo,
  UnhandledBadge,
} from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { bucketLabel, fmtNumber, fmtTs } from "@/lib/time";
import { levelVar } from "@/lib/levels";
import { term } from "@/lib/query";
import { ExceptionView } from "./exception-view";
import { Breadcrumbs } from "./breadcrumbs";
import { ActivityTab, StateQualifiers, WorkflowActions } from "./workflow";
import { LogList } from "@/features/logs/log-list";

const ISSUE_DEFAULT = { from: "-7d", to: "" };
const TABS = [
  "exception",
  "breadcrumbs",
  "request",
  "context",
  "tags",
  "logs",
  "activity",
  "raw",
] as const;

export function IssueDetailPage() {
  const { id } = useParams();
  const { range, setRange } = useRange(ISSUE_DEFAULT);
  const { get, set } = useUrlState();
  const detail = useIssue(id, { from: range.from, to: range.to });
  const trend = useIssueTrend(id, { from: range.from, to: range.to });
  const eventId = get("event");
  const occQ = useOccurrence(eventId || undefined);
  const occ: Occurrence | undefined = eventId ? occQ.data : detail.data?.latest;
  const tab = (get("tab", "exception") as (typeof TABS)[number]) || "exception";
  const nav = useNavigate();

  if (detail.isError)
    return <ErrorState error={detail.error} retry={() => detail.refetch()} />;
  const issue = detail.data?.issue;

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            <Link
              to="/issues"
              className="text-ink-3 hover:text-ink"
              title="Back to issues"
            >
              <ArrowLeft size={16} />
            </Link>
            <span className="truncate">
              {issue?.title || issue?.exception_type || "Issue"}
            </span>
          </span>
        }
        sub={
          issue?.culprit && <span className="font-mono">{issue.culprit}</span>
        }
      >
        {issue && <WorkflowActions issue={issue} />}
        <TimeRangePicker
          value={range}
          onChange={setRange}
          onRefresh={() => (detail.refetch(), trend.refetch())}
          refreshing={detail.isFetching}
        />
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {!issue ? (
          <Skeleton rows={6} className="p-4" />
        ) : (
          <div className="mx-auto flex max-w-[1400px] flex-col gap-3 p-4">
            <IssueSummaryBar d={detail.data!} />
            <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_320px]">
              <div className="min-w-0 space-y-3">
                <Card>
                  <CardHeader
                    title="Occurrences over time"
                    sub={`${fmtNumber(issue.occurrence_count)} total${trend.data?.bucket_seconds ? ` · ${bucketLabel(trend.data.bucket_seconds)} buckets` : ""}`}
                  />
                  <div className="p-2">
                    <TrendChart
                      points={trend.data?.points ?? detail.data?.trend}
                      color={levelVar(issue.level)}
                      label="occurrences"
                    />
                  </div>
                </Card>
                <Card>
                  <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-1.5">
                    <OccurrenceNav
                      occ={occ}
                      issueId={id!}
                      isLatest={!eventId}
                      onSelect={(ev) => set({ event: ev || undefined })}
                    />
                  </div>
                  <Tabs
                    size="sm"
                    className="px-2"
                    value={tab}
                    onChange={(t) =>
                      set({ tab: t === "exception" ? undefined : t }, true)
                    }
                    items={[
                      { id: "exception", label: "Exception" },
                      {
                        id: "breadcrumbs",
                        label: "Breadcrumbs",
                        count: occ?.breadcrumbs?.length,
                      },
                      { id: "request", label: "Request" },
                      { id: "context", label: "User & runtime" },
                      { id: "tags", label: "Tags & attrs" },
                      { id: "logs", label: "Logs" },
                      { id: "activity", label: "Activity" },
                      { id: "raw", label: "Raw" },
                    ]}
                  />
                  <div className="p-3">
                    {tab === "activity" ? (
                      <ActivityTab issueId={id!} />
                    ) : !occ ? (
                      occQ.isError ? (
                        <ErrorState error={occQ.error} />
                      ) : (
                        <Skeleton rows={5} />
                      )
                    ) : (
                      <OccurrenceTab
                        tab={tab}
                        occ={occ}
                        onFilterLogs={(q) =>
                          nav(
                            `/logs?q=${encodeURIComponent(q)}&from=${encodeURIComponent(range.from)}${range.to ? `&to=${encodeURIComponent(range.to)}` : ""}`,
                          )
                        }
                      />
                    )}
                  </div>
                </Card>
              </div>
              <div className="space-y-3">
                <Card>
                  <CardHeader title="Last occurrence" />
                  {occ ? (
                    <OccurrenceMeta occ={occ} />
                  ) : (
                    <Skeleton rows={4} className="p-3" />
                  )}
                </Card>
                <Breakdowns d={detail.data!} />
                <RecentOccurrences
                  issueId={id!}
                  current={occ?.event_id}
                  onSelect={(ev) => set({ event: ev })}
                />
              </div>
            </div>
          </div>
        )}
      </div>
    </>
  );
}

function IssueSummaryBar({ d }: { d: IssueDetail }) {
  const it = d.issue;
  const handled = d.breakdowns.handled;
  const unhandled = handled?.find(
    (h) => h.value === "false" || h.value === "unhandled",
  );
  return (
    <Card className="flex flex-wrap items-center gap-x-5 gap-y-2 px-4 py-2.5">
      <div className="flex items-center gap-2">
        <StatusBadge status={it.status} regressed={it.is_regressed} />
        <StateQualifiers issue={it} />
        {unhandled && <UnhandledBadge />}
        <LevelBadge level={it.level} />
      </div>
      <Stat label="Events" value={fmtNumber(it.occurrence_count)} />
      <Stat
        label="Users"
        value={
          <span className="inline-flex items-center gap-1">
            <Users size={12} className="text-ink-3" />
            {fmtNumber(it.affected_users)}
          </span>
        }
      />
      <Stat label="First seen" value={<TimeAgo iso={it.first_seen} />} />
      <Stat label="Last seen" value={<TimeAgo iso={it.last_seen} />} />
      <div className="ml-auto flex flex-wrap items-center gap-1.5 text-xs">
        <Badge>{it.service}</Badge>
        {it.env && <Badge tone="outline">{it.env}</Badge>}
        {it.last_release && (
          <Badge tone="outline">release {it.last_release}</Badge>
        )}
        <span className="font-mono text-[10px] text-ink-3" title="issue id">
          {it.issue_id.slice(0, 12)}
        </span>
        <CopyButton text={it.issue_id} label="Copy issue id" />
      </div>
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="leading-tight">
      <div className="text-[10px] font-medium uppercase tracking-wide text-ink-3">
        {label}
      </div>
      <div className="text-[13px] font-semibold tabular text-ink">{value}</div>
    </div>
  );
}

function OccurrenceNav({
  occ,
  issueId,
  isLatest,
  onSelect,
}: {
  occ?: Occurrence;
  issueId: string;
  isLatest: boolean;
  onSelect: (ev: string | null) => void;
}) {
  return (
    <div className="flex w-full items-center justify-between gap-2 text-xs">
      <div className="flex items-center gap-2 text-ink-2">
        <span className="font-medium text-ink">
          {isLatest ? "Latest occurrence" : "Occurrence"}
        </span>
        {occ && (
          <>
            <span className="tabular">{fmtTs(occ.ts, { ms: true })}</span>
            <span className="font-mono text-[10px] text-ink-3">
              {occ.event_id.slice(0, 12)}
            </span>
            <CopyButton
              text={`${location.origin}/ui/issues/${issueId}?event=${occ.event_id}`}
              label="Copy link to this occurrence"
            />
          </>
        )}
      </div>
      <div className="flex items-center gap-1">
        <Button
          size="xs"
          icon={<ChevronLeft size={12} />}
          disabled={!occ?.prev_event_id}
          onClick={() => onSelect(occ!.prev_event_id!)}
          title="Older occurrence"
        >
          Older
        </Button>
        <Button
          size="xs"
          disabled={!occ?.next_event_id}
          onClick={() => onSelect(occ!.next_event_id!)}
          title="Newer occurrence"
        >
          Newer
          <ChevronRight size={12} />
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

function OccurrenceTab({
  tab,
  occ,
  onFilterLogs,
}: {
  tab: string;
  occ: Occurrence;
  onFilterLogs: (q: string) => void;
}) {
  const filterActions = useMemo(
    () => ({
      onFilter: (k: string, v: string) => onFilterLogs(term(k, v)),
      onExclude: (k: string, v: string) => onFilterLogs(term(k, v, true)),
    }),
    [onFilterLogs],
  );
  switch (tab) {
    case "breadcrumbs":
      return <Breadcrumbs items={occ.breadcrumbs} anchorTs={occ.ts} />;
    case "request": {
      const h = occ.http;
      if (!h && !occ.http_method && !occ.http_route)
        return (
          <EmptyState
            title="No request context"
            hint="Requests are attached by the Flask / FastAPI integrations and OTLP HTTP attributes."
            className="py-6"
          />
        );
      const method = h?.method || occ.http_method;
      const route = h?.route || occ.http_route;
      const status = h?.status || occ.http_status;
      return (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2 font-mono text-[13px]">
            <Badge tone="accent">{method}</Badge>
            <span className="text-ink">{h?.url || route}</span>
            {status ? (
              <Badge tone={status >= 500 ? "neutral" : "outline"}>
                {status}
              </Badge>
            ) : null}
          </div>
          <KeyValueList
            entries={{
              route,
              query: h?.query,
              client_ip: h?.client_ip,
              remote_addr: h?.remote_addr,
              server_addr: h?.server_addr,
              user_agent: h?.user_agent,
              request_id: occ.request_id,
              trace_id: occ.trace_id,
              span_id: occ.span_id,
            }}
            actions={{
              onFilter: (k, v) =>
                onFilterLogs(term(k === "route" ? "http.route" : k, v)),
            }}
          />
          {occ.trace_id && (
            <Link
              to={`/traces/${occ.trace_id}?at=${encodeURIComponent(occ.ts)}`}
              className="inline-flex h-6 items-center gap-1 rounded-md border border-border bg-surface px-1.5 text-[11px] font-medium text-ink hover:bg-surface-2"
            >
              <GitBranch size={11} /> View trace
            </Link>
          )}
          {h?.headers && Object.keys(h.headers).length > 0 && (
            <Section title="Headers">
              <KeyValueList entries={h.headers} />
            </Section>
          )}
          {h?.body && (
            <Section title="Body">
              <pre className="max-h-80 overflow-auto rounded-md border border-border bg-surface-2 p-2 font-mono text-xs text-ink">
                {prettyMaybe(h.body)}
              </pre>
            </Section>
          )}
        </div>
      );
    }
    case "context":
      return (
        <div className="grid gap-4 md:grid-cols-2">
          <Section title="User">
            <KeyValueList
              entries={{
                ...(occ.user ?? {}),
                id_hash: occ.user_id_hash || occ.user?.id_hash,
              }}
              emptyText="no user attached"
              actions={{
                onFilter: (k, v) =>
                  onFilterLogs(
                    term(k === "id_hash" ? "user" : `attrs.user.${k}`, v),
                  ),
              }}
            />
          </Section>
          <Section title="Runtime">
            <KeyValueList
              entries={{
                ...(occ.runtime ?? {}),
                hostname: occ.hostname,
                release: occ.release,
                sdk: occ.sdk ? `${occ.sdk.name} ${occ.sdk.version}` : undefined,
                source: occ.source,
                mechanism: occ.mechanism
                  ? `${occ.mechanism.type}${occ.mechanism.handled === false ? " (unhandled)" : ""}`
                  : undefined,
              }}
              actions={{
                onFilter: (k, v) =>
                  k === "hostname" || k === "release"
                    ? onFilterLogs(term(k === "hostname" ? "host" : k, v))
                    : undefined,
              }}
            />
          </Section>
        </div>
      );
    case "tags":
      return (
        <div className="grid gap-4 md:grid-cols-2">
          <Section title="Tags">
            <KeyValueList
              entries={occ.tags}
              keyPrefix="tags."
              actions={filterActions}
              emptyText="no tags"
            />
          </Section>
          <Section title="Attributes">
            <KeyValueList
              entries={occ.attrs}
              keyPrefix="attrs."
              actions={filterActions}
              emptyText="no attributes"
            />
          </Section>
          {occ.extra !== undefined && occ.extra !== null && (
            <Section title="Extra" className="md:col-span-2">
              <JsonView value={occ.extra} />
            </Section>
          )}
        </div>
      );
    case "logs":
      return <IssueLogs occ={occ} />;
    case "raw":
      return <JsonView value={occ} defaultOpen={1} />;
    default:
      return <ExceptionView occ={occ} />;
  }
}

function IssueLogs({ occ }: { occ: Occurrence }) {
  const [mode, setMode] = useState<"request" | "issue">(
    occ.request_id ? "request" : "issue",
  );
  const q =
    mode === "request" && occ.request_id
      ? `request_id:${occ.request_id}`
      : `issue:${occ.issue_id}`;
  const center = new Date(occ.ts).getTime();
  const range =
    mode === "request"
      ? {
          from: new Date(center - 10 * 60_000).toISOString(),
          to: new Date(center + 10 * 60_000).toISOString(),
        }
      : { from: "-7d", to: "" };
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1 text-xs">
          <Button
            size="xs"
            active={mode === "request"}
            disabled={!occ.request_id}
            onClick={() => setMode("request")}
            title={
              occ.request_id
                ? "Lines sharing this request id"
                : "No request id on this occurrence"
            }
          >
            Same request
          </Button>
          <Button
            size="xs"
            active={mode === "issue"}
            onClick={() => setMode("issue")}
          >
            All lines for this issue
          </Button>
        </div>
        <Link
          to={`/logs?q=${encodeURIComponent(q)}&from=${encodeURIComponent(range.from)}${range.to ? `&to=${encodeURIComponent(range.to)}` : ""}`}
          className="inline-flex items-center gap-1 text-xs text-accent hover:underline"
        >
          Open in Logs <ExternalLink size={11} />
        </Link>
      </div>
      <LogList
        q={q}
        from={range.from}
        to={range.to}
        highlightEventId={occ.event_id}
        limit={200}
        dense
      />
    </div>
  );
}

function OccurrenceMeta({ occ }: { occ: Occurrence }) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 px-3 py-2 text-xs">
      {(
        [
          ["Time", fmtTs(occ.ts, { ms: true })],
          ["Service", occ.service],
          ["Env", occ.env],
          ["Logger", occ.logger],
          ["Release", occ.release],
          ["Host", occ.hostname],
          ["Request", occ.request_id],
          [
            "Trace",
            occ.trace_id ? (
              <Link
                key="trace"
                to={`/traces/${occ.trace_id}?at=${encodeURIComponent(occ.ts)}`}
                className="text-accent hover:underline"
              >
                {occ.trace_id}
              </Link>
            ) : (
              ""
            ),
          ],
          ["Source", occ.source],
          ["SDK", occ.sdk ? `${occ.sdk.name} ${occ.sdk.version}` : ""],
        ] as [string, React.ReactNode][]
      )
        .filter(([, v]) => v)
        .map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-ink-3">{k}</dt>
            <dd
              className="min-w-0 truncate font-mono text-ink"
              title={typeof v === "string" ? v : undefined}
            >
              {v}
            </dd>
          </div>
        ))}
    </dl>
  );
}

function Breakdowns({ d }: { d: IssueDetail }) {
  const entries = Object.entries(d.breakdowns).filter(([k]) => k !== "handled");
  if (entries.length === 0) return null;
  const labels: Record<string, string> = {
    env: "Environment",
    release: "Release",
    http_route: "Route",
    hostname: "Host",
    user: "User",
  };
  return (
    <Card>
      <CardHeader title="Breakdown" sub="in selected range" />
      <div className="divide-y divide-border">
        {entries.map(([k, items]) => (
          <BreakdownRow key={k} label={labels[k] ?? k} items={items} />
        ))}
      </div>
    </Card>
  );
}

function BreakdownRow({
  label,
  items,
}: {
  label: string;
  items: FacetCount[];
}) {
  const total = items.reduce((n, i) => n + i.count, 0) || 1;
  return (
    <div className="px-3 py-2">
      <div className="mb-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">
        {label}
      </div>
      <ul className="space-y-1">
        {items.map((i) => (
          <li key={i.value} className="text-xs">
            <div className="flex items-center justify-between gap-2">
              <span
                className="min-w-0 truncate font-mono text-ink"
                title={i.value}
              >
                {i.value || "—"}
              </span>
              <span className="tabular text-ink-2">
                {fmtNumber(i.count)}{" "}
                <span className="text-ink-3">
                  ({Math.round((i.count / total) * 100)}%)
                </span>
              </span>
            </div>
            <div className="mt-0.5 h-1 w-full rounded bg-surface-3">
              <div
                className="h-1 rounded bg-accent"
                style={{ width: `${Math.max(2, (i.count / total) * 100)}%` }}
              />
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function RecentOccurrences({
  issueId,
  current,
  onSelect,
}: {
  issueId: string;
  current?: string;
  onSelect: (ev: string) => void;
}) {
  const q = useOccurrences(issueId, 30);
  return (
    <Card>
      <CardHeader
        title="Recent occurrences"
        sub={q.data ? `${q.data.items.length} shown` : undefined}
      />
      {q.isPending ? (
        <Skeleton rows={5} className="p-3" />
      ) : (
        <ul className="max-h-96 divide-y divide-border overflow-y-auto">
          {q.data?.items.map((o) => (
            <li key={o.event_id}>
              <button
                type="button"
                onClick={() => onSelect(o.event_id)}
                className={clsx(
                  "flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-surface-2",
                  o.event_id === current && "bg-accent-soft",
                )}
              >
                <span className="tabular text-ink">{fmtTs(o.ts)}</span>
                <span className="min-w-0 flex-1 truncate text-ink-3">
                  {o.http_route
                    ? `${o.http_method} ${o.http_route}`
                    : o.hostname || o.env}
                </span>
                {o.http_status ? (
                  <span className="tabular text-ink-3">{o.http_status}</span>
                ) : null}
              </button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function Section({
  title,
  children,
  className,
}: {
  title: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section className={className}>
      <h4 className="mb-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">
        {title}
      </h4>
      {children}
    </section>
  );
}

function prettyMaybe(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}
