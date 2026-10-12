import { useEffect, useMemo, useState } from "react";
import { Ban, CheckCircle2, RotateCcw, UserCircle2 } from "lucide-react";
import { useBulkIssueState, useIssues, useLogFacets, type IssuesParams } from "@/api/hooks";
import type { FacetCount, IssueSummary } from "@/api/types";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { Sparkline } from "@/components/charts/sparkline";
import { Badge, Button, Card, Checkbox, DataTable, EmptyState, MetaLine, Page, Pagination, SearchInput, Select, SelectionBar, StatusBadge, Tabs, Term, TimeAgo, type Column } from "@/components/ui";
import { StateQualifiers } from "./workflow";
import { isNewIssue, issueHeadline } from "./issue-text";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { fmtNumber } from "@/lib/time";
import { pluralize } from "@/lib/format";
import { term as glossary } from "@/lib/glossary";

const ISSUES_DEFAULT = { from: "-7d", to: "" };

const TAB_EMPTY: Record<string, { title: string; hint: string }> = {
  open: { title: "No unresolved issues", hint: "Nothing your apps reported is waiting for a fix in this time range." },
  regressed: { title: "Nothing came back", hint: "No issue marked as fixed has happened again in this time range." },
  resolved: { title: "No resolved issues", hint: "Issues you mark as fixed show up here. You'll be told if one comes back." },
  ignored: { title: "No ignored issues", hint: "Issues you choose to ignore show up here. They are still recorded." },
  all: { title: "No issues in this time range", hint: "Issues appear when your apps report crashes or log error messages." },
};

export function IssuesPage() {
  const { range, setRange } = useRange(ISSUES_DEFAULT);
  const { get, set } = useUrlState();
  const status = get("status", "open");
  const q = get("q");
  const sort = get("sort", "last_seen");
  const assignee = get("assignee");
  const service = get("service");
  const env = get("env");
  const release = get("release");
  const offset = Number(get("offset", "0")) || 0;
  const limit = 50;
  const params: IssuesParams = useMemo(
    () => ({ status: status === "all" ? "" : status, q, sort, assignee, from: range.from, to: range.to, limit, offset, service, env, release, level: get("level") }),
    [status, q, sort, assignee, range.from, range.to, offset, service, env, release, get],
  );
  const issues = useIssues(params, status === "open" && !range.to ? 30_000 : false);
  const facets = useLogFacets({ from: range.from, to: range.to || undefined, fields: ["service", "env", "release"], attr_keys: 0 });
  const bulk = useBulkIssueState();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [draft, setDraft] = useState(q);
  useEffect(() => setDraft(q), [q]);
  useEffect(() => setSelected(new Set()), [status, q, offset, service, env, release]);

  const items = issues.data?.items ?? [];
  const counts = issues.data?.counts ?? {};
  const allSelected = items.length > 0 && items.every((i) => selected.has(i.issue_id));
  const filtered = !!(q || assignee || service || env || release);
  const act = (st: string) => {
    if (selected.size === 0) return;
    bulk.mutate({ issue_ids: [...selected], status: st }, { onSuccess: () => setSelected(new Set()) });
  };
  const toggle = (id: string, on: boolean) =>
    setSelected((s) => {
      const n = new Set(s);
      if (on) n.add(id);
      else n.delete(id);
      return n;
    });

  const columns: Column<IssueSummary>[] = [
    {
      id: "select",
      width: "20px",
      header: <Checkbox checked={allSelected} aria-label="Select all on this page" onChange={(e) => setSelected(e.target.checked ? new Set(items.map((i) => i.issue_id)) : new Set())} />,
      cell: (it) => <Checkbox checked={selected.has(it.issue_id)} aria-label="Select issue" onChange={(e) => toggle(it.issue_id, e.target.checked)} />,
      className: "self-start pt-0.5",
    },
    { id: "issue", header: "Issue", cell: (it) => <IssueCell it={it} showStatus={status === "all"} hideCameBack={status === "regressed"} /> },
    {
      id: "trend",
      header: <span title="Times per hour over the last 24 hours">Last 24 hours</span>,
      width: "112px",
      hideBelow: "md",
      cell: (it) => <Sparkline values={it.sparkline} width={104} height={24} title={`${fmtNumber((it.sparkline ?? []).reduce((a, b) => a + b, 0))} times in the last 24 hours`} />,
    },
    {
      id: "times",
      header: <Term k="events">Times</Term>,
      width: "72px",
      align: "right",
      cell: (it) => (
        <span className="flex flex-col items-end">
          <span className="font-medium text-ink">{fmtNumber(it.occurrence_count)}</span>
          {/* On a phone the People column steps out, so its number sits under the count. */}
          <span className="whitespace-nowrap text-xs text-ink-3 sm:hidden">{pluralize(it.affected_users, "person", "people")}</span>
        </span>
      ),
    },
    { id: "people", header: <Term k="users">People</Term>, width: "64px", align: "right", hideBelow: "sm", cell: (it) => <span className="text-ink-2">{fmtNumber(it.affected_users)}</span> },
  ];

  return (
    <Page
      title="Issues"
      description="Errors your apps reported, grouped by cause. Resolve them when they're fixed; you'll be told if they come back."
      actions={<TimeRangePicker value={range} onChange={setRange} onRefresh={() => issues.refetch()} refreshing={issues.isFetching} />}
    >
      <Card className="overflow-hidden">
        <div className="px-4 pt-2">
          <Tabs
            items={[
              { id: "open", label: glossary("unresolved").label, count: counts.open, title: glossary("unresolved").explain },
              { id: "regressed", label: glossary("regressed").label, count: counts.regressed, title: `${glossary("regressed").explain} (regressed)` },
              { id: "resolved", label: "Resolved", count: counts.resolved },
              { id: "ignored", label: "Ignored", count: counts.ignored },
              { id: "all", label: "All", count: counts.all },
            ]}
            value={status}
            onChange={(s) => set({ status: s === "open" ? undefined : s, offset: undefined })}
          />
        </div>
        {/* Two filters a row on a phone; one line that wraps on wider screens. */}
        <div className="grid grid-cols-2 gap-2 border-b border-border px-4 py-3 sm:flex sm:flex-wrap sm:items-center [&>select]:max-sm:w-full [&>select]:max-sm:min-w-0 [&>select]:max-sm:max-w-none">
          <form
            className="col-span-2 min-w-56 flex-1"
            onSubmit={(e) => {
              e.preventDefault();
              set({ q: draft || undefined, offset: undefined });
            }}
          >
            <SearchInput value={draft} onChange={(v) => (setDraft(v), v === "" && q && set({ q: undefined, offset: undefined }))} placeholder="Search errors and messages" aria-label="Search issues" />
          </form>
          <FacetSelect label="App" all="All apps" value={service} options={facets.data?.fields.service} onChange={(v) => set({ service: v || undefined, offset: undefined })} />
          <FacetSelect label="Environment" all="All environments" value={env} options={facets.data?.fields.env} onChange={(v) => set({ env: v || undefined, offset: undefined })} />
          <FacetSelect label="Version" all="All versions" value={release} options={facets.data?.fields.release} onChange={(v) => set({ release: v || undefined, offset: undefined })} />
          <Select value={assignee} onChange={(e) => set({ assignee: e.target.value || undefined, offset: undefined })} className="w-auto min-w-36" aria-label="Assigned to">
            <option value="">Anyone</option>
            <option value="me">Assigned to me</option>
            <option value="none">Not assigned</option>
          </Select>
          <Select value={sort} onChange={(e) => set({ sort: e.target.value === "last_seen" ? undefined : e.target.value, offset: undefined })} className="w-auto min-w-36" aria-label="Sort by">
            <option value="last_seen">Most recent</option>
            <option value="first_seen">Newest first</option>
            <option value="count">Most frequent</option>
            <option value="users">Most people affected</option>
          </Select>
        </div>
        <SelectionBar count={selected.size} noun={selected.size === 1 ? "issue selected" : "issues selected"} onClear={() => setSelected(new Set())}>
          {status !== "resolved" && (
            <Button size="xs" icon={<CheckCircle2 size={13} />} onClick={() => act("resolved")} loading={bulk.isPending}>
              Resolve
            </Button>
          )}
          {status !== "ignored" && (
            <Button size="xs" icon={<Ban size={13} />} onClick={() => act("ignored")}>
              Ignore
            </Button>
          )}
          {status !== "open" && status !== "regressed" && (
            <Button size="xs" icon={<RotateCcw size={13} />} onClick={() => act("open")}>
              Reopen
            </Button>
          )}
        </SelectionBar>
        <DataTable
          label="Issues"
          columns={columns}
          rows={issues.data ? items : undefined}
          rowKey={(it) => it.issue_id}
          rowHref={(it) => `/issues/${it.issue_id}`}
          selected={(it) => selected.has(it.issue_id)}
          loading={issues.isPending}
          error={issues.isError ? issues.error : undefined}
          onRetry={() => issues.refetch()}
          stickyHeader={false}
          rowClassName={() => "items-start py-3"}
          empty={
            filtered ? (
              <EmptyState
                title="No issues match these filters"
                hint="Try a wider time range, or clear the search and filters."
                action={
                  <Button onClick={() => set({ q: undefined, assignee: undefined, service: undefined, env: undefined, release: undefined, offset: undefined })}>Clear filters</Button>
                }
              />
            ) : (
              <EmptyState icon={status === "open" ? <CheckCircle2 size={18} className="text-good-text" /> : undefined} title={TAB_EMPTY[status]?.title ?? "No issues"} hint={TAB_EMPTY[status]?.hint} />
            )
          }
          footer={items.length > 0 ? <Pagination offset={offset} limit={limit} count={items.length} onChange={(o) => set({ offset: o || undefined })} /> : undefined}
        />
      </Card>
    </Page>
  );
}

function FacetSelect({ label, all, value, options, onChange }: { label: string; all: string; value: string; options?: FacetCount[]; onChange: (v: string) => void }) {
  const values = (options ?? []).filter((o) => o.value !== "");
  if (value && !values.some((o) => o.value === value)) values.unshift({ value, count: 0 });
  if (values.length === 0 && !value) return null;
  return (
    <Select value={value} onChange={(e) => onChange(e.target.value)} className="w-auto min-w-32 max-w-52" aria-label={label} title={label}>
      <option value="">{all}</option>
      {values.map((o) => (
        <option key={o.value} value={o.value}>
          {o.value}
        </option>
      ))}
    </Select>
  );
}

function IssueCell({ it, showStatus, hideCameBack }: { it: IssueSummary; showStatus: boolean; hideCameBack: boolean }) {
  const { headline, message } = issueHeadline(it);
  const cameBack = it.is_regressed && it.status === "open";
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <span className="min-w-0 truncate text-[14px] font-semibold text-ink">{headline}</span>
        {isNewIssue(it) && !cameBack && (
          <Badge tone="accent" title="First seen in the last 24 hours">
            New
          </Badge>
        )}
        {showStatus ? <StatusBadge status={it.status} regressed={it.is_regressed} /> : cameBack && !hideCameBack && <StatusBadge status="open" regressed />}
        <StateQualifiers issue={it} />
      </div>
      {message && <div className="mt-0.5 truncate text-[13px] text-ink-2">{message}</div>}
      <MetaLine className="mt-1">
        {it.service}
        {it.env}
        {it.last_release && `version ${it.last_release}`}
        {it.culprit && <span className="font-mono text-[11px]" title="Where it happened">{it.culprit}</span>}
        <span>
          last seen <TimeAgo iso={it.last_seen} />
        </span>
        {it.assignee && (
          <span className="inline-flex items-center gap-1" title={`Assigned to ${it.assignee}`}>
            <UserCircle2 size={12} className="text-accent" />
            {it.assignee}
          </span>
        )}
      </MetaLine>
    </div>
  );
}
