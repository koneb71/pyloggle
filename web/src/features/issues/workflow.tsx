import { useState } from "react";
import clsx from "clsx";
import { Ban, Check, CheckCircle2, ChevronDown, Clock, MessageSquare, RotateCcw, Tag, UserCircle2, UserMinus } from "lucide-react";
import { useAddComment, useAssignIssue, useAssignees, useIssueActivity, useIssueState } from "@/api/hooks";
import type { IssueActivity, IssueSummary } from "@/api/types";
import { Badge, Button, Dialog, EmptyState, Field, Input, MenuItem, Popover, Skeleton, Textarea, TimeAgo } from "@/components/ui";
import { fmtTs } from "@/lib/time";

/** Resolve / ignore menus and the assignee picker for one issue. */
export function WorkflowActions({ issue }: { issue: IssueSummary }) {
  const m = useIssueState();
  const [releaseDialog, setReleaseDialog] = useState(false);
  const [countDialog, setCountDialog] = useState(false);
  const go = (change: Parameters<typeof m.mutate>[0]) => m.mutate(change);
  return (
    <div className="flex items-center gap-1">
      <AssigneePicker issue={issue} />
      {issue.status !== "resolved" && (
        <Popover
          align="end"
          width={260}
          trigger={({ toggle, open }) => (
            <Button variant="primary" active={open} icon={<CheckCircle2 size={13} />} onClick={toggle} loading={m.isPending}>
              Resolve <ChevronDown size={12} className="opacity-70" />
            </Button>
          )}
        >
          {(close) => (
            <div>
              <MenuItem icon={<Check size={13} />} onClick={() => (close(), go({ id: issue.issue_id, status: "resolved" }))}>
                Resolve
              </MenuItem>
              <MenuItem icon={<Tag size={13} />} disabled={!issue.last_release} onClick={() => (close(), go({ id: issue.issue_id, status: "resolved", resolved_in_release: "next" }))}>
                <span className="min-w-0 flex-1">
                  Resolve in the next release
                  <span className="block truncate text-[11px] text-ink-3">{issue.last_release ? `newer than ${issue.last_release} reopens it` : "no release on this issue"}</span>
                </span>
              </MenuItem>
              <MenuItem icon={<Tag size={13} />} onClick={() => (close(), setReleaseDialog(true))}>
                Resolve in release…
              </MenuItem>
            </div>
          )}
        </Popover>
      )}
      {issue.status !== "ignored" && (
        <Popover
          align="end"
          width={240}
          trigger={({ toggle, open }) => (
            <Button active={open} icon={<Ban size={13} />} onClick={toggle}>
              Ignore <ChevronDown size={12} className="text-ink-3" />
            </Button>
          )}
        >
          {(close) => (
            <div>
              <MenuItem icon={<Ban size={13} />} onClick={() => (close(), go({ id: issue.issue_id, status: "ignored" }))}>
                Ignore forever
              </MenuItem>
              {[
                ["for 1 hour", 3600],
                ["for 24 hours", 86400],
                ["for 7 days", 7 * 86400],
              ].map(([label, secs]) => (
                <MenuItem key={label} icon={<Clock size={13} />} onClick={() => (close(), go({ id: issue.issue_id, status: "ignored", snooze_for_seconds: Number(secs) }))}>
                  Ignore {label}
                </MenuItem>
              ))}
              <MenuItem icon={<Clock size={13} />} onClick={() => (close(), setCountDialog(true))}>
                Ignore until N more occurrences…
              </MenuItem>
            </div>
          )}
        </Popover>
      )}
      {issue.status !== "open" && (
        <Button icon={<RotateCcw size={13} />} onClick={() => go({ id: issue.issue_id, status: "open" })} loading={m.isPending}>
          Reopen
        </Button>
      )}
      {releaseDialog && (
        <PromptDialog
          title="Resolve in release"
          label="Release"
          hint="The issue regresses only when a newer release reports it (semantic versions compare numerically, other names by first-seen time)."
          initial={issue.last_release ?? ""}
          onClose={() => setReleaseDialog(false)}
          onSubmit={(v) => go({ id: issue.issue_id, status: "resolved", resolved_in_release: v })}
        />
      )}
      {countDialog && (
        <PromptDialog
          title="Ignore until more occurrences"
          label="Occurrences"
          hint="The issue reopens once this many more events arrive."
          initial="100"
          type="number"
          onClose={() => setCountDialog(false)}
          onSubmit={(v) => go({ id: issue.issue_id, status: "ignored", snooze_count: Math.max(1, Number(v) || 1) })}
        />
      )}
    </div>
  );
}

function PromptDialog({ title, label, hint, initial, type = "text", onClose, onSubmit }: { title: string; label: string; hint?: string; initial: string; type?: string; onClose: () => void; onSubmit: (v: string) => void }) {
  const [v, setV] = useState(initial);
  return (
    <Dialog open onClose={onClose} title={title}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (!v.trim()) return;
          onSubmit(v.trim());
          onClose();
        }}
      >
        <Field label={label} hint={hint}>
          <Input autoFocus type={type} value={v} onChange={(e) => setV(e.target.value)} min={type === "number" ? 1 : undefined} />
        </Field>
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary">
            Apply
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

export function AssigneePicker({ issue, compact }: { issue: IssueSummary; compact?: boolean }) {
  const people = useAssignees();
  const assign = useAssignIssue();
  const [draft, setDraft] = useState("");
  const current = issue.assignee ?? "";
  const suggestions = (people.data?.items ?? []).filter((p) => p !== current && (!draft || p.toLowerCase().includes(draft.toLowerCase())));
  return (
    <Popover
      align="end"
      width={260}
      trigger={({ toggle, open }) => (
        <Button active={open} onClick={toggle} icon={<UserCircle2 size={13} className={current ? "text-accent" : "text-ink-3"} />} title={current ? `Assigned to ${current}` : "Unassigned"} className={clsx(compact && "px-1.5")}>
          {!compact && <span className="max-w-32 truncate">{current || "Assign"}</span>}
          {!compact && <ChevronDown size={12} className="text-ink-3" />}
        </Button>
      )}
    >
      {(close) => (
        <div>
          <form
            className="px-1 pb-1"
            onSubmit={(e) => {
              e.preventDefault();
              if (!draft.trim()) return;
              assign.mutate({ id: issue.issue_id, assignee: draft.trim() });
              setDraft("");
              close();
            }}
          >
            <Input autoFocus value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="name or e-mail…" className="h-7 text-xs" />
          </form>
          {people.data?.me && people.data.me !== current && (
            <MenuItem icon={<UserCircle2 size={13} />} onClick={() => (assign.mutate({ id: issue.issue_id, assignee: "me" }), close())}>
              Assign to me ({people.data.me})
            </MenuItem>
          )}
          {suggestions.slice(0, 8).map((p) => (
            <MenuItem key={p} icon={<span className="inline-block w-[13px]" />} onClick={() => (assign.mutate({ id: issue.issue_id, assignee: p }), close())}>
              {p}
            </MenuItem>
          ))}
          {current && (
            <>
              <div className="my-1 h-px bg-border" />
              <MenuItem icon={<UserMinus size={13} />} onClick={() => (assign.mutate({ id: issue.issue_id, assignee: "" }), close())}>
                Unassign {current}
              </MenuItem>
            </>
          )}
        </div>
      )}
    </Popover>
  );
}

/** Badges that explain a snoozed or release-resolved state. */
export function StateQualifiers({ issue }: { issue: IssueSummary }) {
  const parts: React.ReactNode[] = [];
  if (issue.status === "ignored" && issue.snooze_until) parts.push(<Badge key="until" tone="outline">snoozed until {fmtTs(issue.snooze_until)}</Badge>);
  if (issue.status === "ignored" && issue.snooze_count) parts.push(<Badge key="count" tone="outline">snoozed until {issue.snooze_count.toLocaleString()} events</Badge>);
  if (issue.status === "resolved" && issue.resolved_in_release) parts.push(<Badge key="rel" tone="outline">resolved in {issue.resolved_in_release}</Badge>);
  if (parts.length === 0) return null;
  return <>{parts}</>;
}

const KIND_LABEL: Record<string, string> = {
  status: "changed status",
  assign: "assigned",
  comment: "commented",
  snooze: "snoozed",
  unsnooze: "reopened",
  regression: "regressed",
  resolve_in_release: "resolved in release",
};

export function ActivityTab({ issueId }: { issueId: string }) {
  const q = useIssueActivity(issueId);
  const add = useAddComment();
  const [body, setBody] = useState("");
  return (
    <div className="space-y-3">
      <form
        className="flex flex-col gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (!body.trim()) return;
          add.mutate({ id: issueId, body: body.trim() }, { onSuccess: () => setBody("") });
        }}
      >
        <Textarea rows={2} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Leave a note for the team…" />
        <div className="flex justify-end">
          <Button type="submit" size="sm" variant="primary" icon={<MessageSquare size={12} />} loading={add.isPending} disabled={!body.trim()}>
            Comment
          </Button>
        </div>
      </form>
      {q.isPending ? (
        <Skeleton rows={4} />
      ) : (q.data?.items.length ?? 0) === 0 ? (
        <EmptyState title="No activity yet" hint="Status changes, assignments, snoozes, regressions and comments show up here." className="py-6" />
      ) : (
        <ol className="relative ml-2 border-l border-border pl-4">
          {q.data!.items.map((a) => (
            <ActivityRow key={a.id} a={a} />
          ))}
        </ol>
      )}
    </div>
  );
}

function ActivityRow({ a }: { a: IssueActivity }) {
  const tone = a.kind === "regression" ? "var(--st-critical)" : a.kind === "comment" ? "var(--accent)" : a.kind === "status" && a.to === "resolved" ? "var(--st-good)" : "var(--text-3)";
  return (
    <li className="relative py-1.5">
      <span className="absolute -left-[21px] top-[11px] h-2.5 w-2.5 rounded-full border-2 border-surface" style={{ background: tone }} aria-hidden />
      <div className="flex flex-wrap items-baseline gap-x-2 text-xs">
        <span className="font-medium text-ink">{a.actor || "someone"}</span>
        <span className="text-ink-2">{KIND_LABEL[a.kind] ?? a.kind}</span>
        {a.kind === "status" && a.from !== a.to && (
          <span className="font-mono text-ink-2">
            {a.from || "open"} → {a.to}
          </span>
        )}
        {a.kind === "assign" && <span className="font-mono text-ink-2">{a.to ? `to ${a.to}` : `(unassigned${a.from ? ` from ${a.from}` : ""})`}</span>}
        {a.kind === "snooze" && <span className="text-ink-2">{a.to}</span>}
        {(a.kind === "regression" || a.kind === "resolve_in_release") && a.release && <span className="font-mono text-ink-2">{a.release}</span>}
        <span className="ml-auto text-ink-3" title={fmtTs(a.ts, { ms: true })}>
          <TimeAgo iso={a.ts} />
        </span>
      </div>
      {a.comment && <div className="mt-0.5 whitespace-pre-wrap break-words text-[13px] text-ink">{a.comment}</div>}
    </li>
  );
}
