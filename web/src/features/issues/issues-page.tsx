import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import clsx from "clsx";
import { Ban, CheckCircle2, RotateCcw, Search, UserCircle2, Users } from "lucide-react";
import { useBulkIssueState, useIssues, type IssuesParams } from "@/api/hooks";
import type { IssueSummary } from "@/api/types";
import { PageHeader } from "@/components/shell/app-shell";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { Sparkline } from "@/components/charts/sparkline";
import { Badge, Button, EmptyState, ErrorState, Input, LevelBadge, Pagination, Select, Skeleton, StatusBadge, Tabs, TimeAgo } from "@/components/ui";
import { StateQualifiers } from "./workflow";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { fmtNumber } from "@/lib/time";
import { levelVar } from "@/lib/levels";

const ISSUES_DEFAULT = { from: "-7d", to: "" };

export function IssuesPage() {
  const { range, setRange } = useRange(ISSUES_DEFAULT);
  const { get, set } = useUrlState();
  const status = get("status", "open");
  const q = get("q");
  const sort = get("sort", "last_seen");
  const assignee = get("assignee");
  const offset = Number(get("offset", "0")) || 0;
  const limit = 50;
  const params: IssuesParams = useMemo(
    () => ({ status: status === "all" ? "" : status, q, sort, assignee, from: range.from, to: range.to, limit, offset, service: get("service"), env: get("env"), level: get("level") }),
    [status, q, sort, assignee, range.from, range.to, offset, get],
  );
  const issues = useIssues(params, status === "open" && !range.to ? 30_000 : false);
  const bulk = useBulkIssueState();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [draft, setDraft] = useState(q);
  useEffect(() => setDraft(q), [q]);
  useEffect(() => setSelected(new Set()), [status, q, offset]);

  const items = issues.data?.items ?? [];
  const counts = issues.data?.counts ?? {};
  const allSelected = items.length > 0 && items.every((i) => selected.has(i.issue_id));

  const act = (st: string) => {
    if (selected.size === 0) return;
    bulk.mutate({ issue_ids: [...selected], status: st }, { onSuccess: () => setSelected(new Set()) });
  };

  return (
    <>
      <PageHeader title="Issues" sub="Grouped exceptions and error-level logs">
        <TimeRangePicker value={range} onChange={setRange} onRefresh={() => issues.refetch()} refreshing={issues.isFetching} />
      </PageHeader>
      <div className="flex flex-none flex-wrap items-center gap-2 border-b border-border bg-surface px-4 pt-2">
        <Tabs
          className="-mb-px flex-1 border-0"
          items={[
            { id: "open", label: "Open", count: counts.open },
            { id: "regressed", label: "Regressed", count: counts.regressed },
            { id: "resolved", label: "Resolved", count: counts.resolved },
            { id: "ignored", label: "Ignored", count: counts.ignored },
            { id: "all", label: "All", count: counts.all },
          ]}
          value={status}
          onChange={(s) => set({ status: s === "open" ? undefined : s, offset: undefined })}
        />
        <form
          className="relative pb-2"
          onSubmit={(e) => {
            e.preventDefault();
            set({ q: draft || undefined, offset: undefined });
          }}
        >
          <Search size={13} className="pointer-events-none absolute left-2 top-[9px] text-ink-3" />
          <Input value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Search title, culprit, type…" className="w-64 pl-7" />
        </form>
        <Select value={assignee} onChange={(e) => set({ assignee: e.target.value || undefined, offset: undefined })} className="mb-2 w-40" aria-label="Assignee">
          <option value="">Any assignee</option>
          <option value="me">Assigned to me</option>
          <option value="none">Unassigned</option>
        </Select>
        <Select value={sort} onChange={(e) => set({ sort: e.target.value === "last_seen" ? undefined : e.target.value, offset: undefined })} className="mb-2 w-36" aria-label="Sort">
          <option value="last_seen">Last seen</option>
          <option value="first_seen">First seen</option>
          <option value="count">Events</option>
          <option value="users">Users</option>
        </Select>
      </div>
      {selected.size > 0 && (
        <div className="flex flex-none items-center gap-2 border-b border-border bg-accent-soft px-4 py-1.5 text-xs text-ink">
          <span className="font-medium">{selected.size} selected</span>
          <Button size="xs" icon={<CheckCircle2 size={12} />} onClick={() => act("resolved")} loading={bulk.isPending}>
            Resolve
          </Button>
          <Button size="xs" icon={<Ban size={12} />} onClick={() => act("ignored")}>
            Ignore
          </Button>
          <Button size="xs" icon={<RotateCcw size={12} />} onClick={() => act("open")}>
            Reopen
          </Button>
          <Button size="xs" variant="ghost" onClick={() => setSelected(new Set())}>
            Clear
          </Button>
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-auto">
        {issues.isError && <ErrorState error={issues.error} retry={() => issues.refetch()} />}
        {issues.isPending ? (
          <Skeleton rows={8} className="p-4" />
        ) : items.length === 0 ? (
          <EmptyState title={status === "open" ? "No open issues" : "No issues match"} hint="Issues are created from exceptions captured by the SDK or OTLP exporters, and from error-level logs." />
        ) : (
          <table className="w-full border-collapse text-[13px]">
            <thead className="sticky top-0 z-10 bg-surface text-[11px] font-medium uppercase tracking-wide text-ink-3 shadow-[0_1px_0_var(--border)]">
              <tr>
                <th className="w-8 px-3 py-2">
                  <input type="checkbox" className="accent-accent" checked={allSelected} aria-label="Select all" onChange={(e) => setSelected(e.target.checked ? new Set(items.map((i) => i.issue_id)) : new Set())} />
                </th>
                <th className="px-2 py-2 text-left">Issue</th>
                <th className="w-28 px-2 py-2 text-left">Trend (24h)</th>
                <th className="w-20 px-2 py-2 text-right">Events</th>
                <th className="w-20 px-2 py-2 text-right">Users</th>
                <th className="w-32 px-2 py-2 text-left">Last seen</th>
                <th className="w-28 px-2 py-2 text-left">First seen</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => (
                <IssueRow key={it.issue_id} it={it} selected={selected.has(it.issue_id)} onSelect={(on) => setSelected((s) => { const n = new Set(s); on ? n.add(it.issue_id) : n.delete(it.issue_id); return n; })} />
              ))}
            </tbody>
          </table>
        )}
        {items.length > 0 && <Pagination offset={offset} limit={limit} count={items.length} onChange={(o) => set({ offset: o || undefined })} />}
      </div>
    </>
  );
}

function IssueRow({ it, selected, onSelect }: { it: IssueSummary; selected: boolean; onSelect: (on: boolean) => void }) {
  const nav = useNavigate();
  const href = `/issues/${it.issue_id}`;
  return (
    <tr
      className={clsx("group cursor-pointer border-b border-border hover:bg-surface-2", selected && "bg-accent-soft/60")}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("a,input,button")) return;
        nav(href);
      }}
    >
      <td className="px-3 py-2 align-top">
        <input type="checkbox" className="accent-accent" checked={selected} onChange={(e) => onSelect(e.target.checked)} aria-label="Select issue" />
      </td>
      <td className="min-w-0 px-2 py-2 align-top">
        <div className="flex items-start gap-2">
          <span className="lvl-bar mt-0.5 h-9" style={{ background: levelVar(it.level) }} aria-hidden />
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <Link to={href} className="truncate font-medium text-ink hover:text-accent" onClick={(e) => e.stopPropagation()}>
                {it.title || it.exception_type || it.last_message || it.issue_id}
              </Link>
              <StatusBadge status={it.status} regressed={it.is_regressed} />
              <StateQualifiers issue={it} />
              {it.assignee && (
                <Badge tone="accent" className="max-w-40 truncate" title={`Assigned to ${it.assignee}`}>
                  <UserCircle2 size={10} /> {it.assignee}
                </Badge>
              )}
            </div>
            <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-ink-3">
              {it.culprit && <span className="truncate font-mono text-ink-2">{it.culprit}</span>}
              <LevelBadge level={it.level} />
              <Badge>{it.service}</Badge>
              {it.env && <Badge tone="outline">{it.env}</Badge>}
              {it.last_release && <Badge tone="outline">{it.last_release}</Badge>}
            </div>
            {it.last_message && it.last_message !== it.title && <div className="mt-0.5 truncate text-xs text-ink-3">{it.last_message}</div>}
          </div>
        </div>
      </td>
      <td className="px-2 py-2 align-top">
        <Sparkline values={it.sparkline} title={`${it.occurrence_count} events`} color={levelVar(it.level)} />
      </td>
      <td className="px-2 py-2 text-right align-top font-medium tabular text-ink">{fmtNumber(it.occurrence_count)}</td>
      <td className="px-2 py-2 text-right align-top tabular text-ink-2">
        <span className="inline-flex items-center gap-1">
          <Users size={11} className="text-ink-3" />
          {fmtNumber(it.affected_users)}
        </span>
      </td>
      <td className="px-2 py-2 align-top text-xs text-ink-2">
        <TimeAgo iso={it.last_seen} />
      </td>
      <td className="px-2 py-2 align-top text-xs text-ink-3">
        <TimeAgo iso={it.first_seen} />
      </td>
    </tr>
  );
}
