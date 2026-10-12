import { useState } from "react";
import { Ban, Check, CheckCircle2, ChevronDown, Clock, MessageSquare, RotateCcw, Tag, UserCircle2, UserMinus } from "lucide-react";
import { useAddComment, useAssignIssue, useAssignees, useIssueActivity, useIssueState } from "@/api/hooks";
import type { IssueActivity, IssueSummary } from "@/api/types";
import { Badge, Button, Dialog, EmptyState, Field, Input, MenuItem, Popover, QueryState, Skeleton, Textarea, TimeAgo } from "@/components/ui";
import { fmtTs, fmtWhen } from "@/lib/time";

/** Resolve / ignore menus and the assignee picker for one issue. */
export function WorkflowActions({ issue }: { issue: IssueSummary }) {
  const m = useIssueState();
  const [releaseDialog, setReleaseDialog] = useState(false);
  const [countDialog, setCountDialog] = useState(false);
  const go = (change: Parameters<typeof m.mutate>[0]) => m.mutate(change);
  return (
    <div className="flex flex-wrap items-center gap-1">
      <AssigneePicker issue={issue} />
      {issue.status !== "resolved" && (
        <Popover
          align="end"
          width={260}
          trigger={({ toggle, open }) => (
            <Button variant="primary" active={open} icon={<CheckCircle2 size={14} />} onClick={toggle} loading={m.isPending}>
              Resolve <ChevronDown size={13} className="opacity-70" />
            </Button>
          )}
        >
          {(close) => (
            <div>
              <MenuItem icon={<Check size={14} />} onClick={() => (close(), go({ id: issue.issue_id, status: "resolved" }))}>
                <span className="min-w-0 flex-1">
                  Resolve now
                  <span className="block text-xs text-ink-3">It reopens if it happens again</span>
                </span>
              </MenuItem>
              <MenuItem icon={<Tag size={14} />} disabled={!issue.last_release} onClick={() => (close(), go({ id: issue.issue_id, status: "resolved", resolved_in_release: "next" }))}>
                <span className="min-w-0 flex-1">
                  Fixed in the next version
                  <span className="block truncate text-xs text-ink-3">{issue.last_release ? `Reopens only if a version after ${issue.last_release} has it` : "No version recorded on this issue"}</span>
                </span>
              </MenuItem>
              <MenuItem icon={<Tag size={14} />} onClick={() => (close(), setReleaseDialog(true))}>
                Fixed in a specific version…
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
            <Button active={open} icon={<Ban size={14} />} onClick={toggle}>
              Ignore <ChevronDown size={13} className="text-ink-3" />
            </Button>
          )}
        >
          {(close) => (
            <div>
              {[
                ["for 1 hour", 3600],
                ["for 24 hours", 86400],
                ["for 7 days", 7 * 86400],
              ].map(([label, secs]) => (
                <MenuItem key={label} icon={<Clock size={14} />} onClick={() => (close(), go({ id: issue.issue_id, status: "ignored", snooze_for_seconds: Number(secs) }))}>
                  Ignore {label}
                </MenuItem>
              ))}
              <MenuItem icon={<Clock size={14} />} onClick={() => (close(), setCountDialog(true))}>
                Ignore until it happens more…
              </MenuItem>
              <div className="my-1 h-px bg-border" />
              <MenuItem icon={<Ban size={14} />} onClick={() => (close(), go({ id: issue.issue_id, status: "ignored" }))}>
                <span className="min-w-0 flex-1">
                  Ignore forever
                  <span className="block text-xs text-ink-3">Still recorded, never reopens</span>
                </span>
              </MenuItem>
            </div>
          )}
        </Popover>
      )}
      {issue.status !== "open" && (
        <Button icon={<RotateCcw size={14} />} onClick={() => go({ id: issue.issue_id, status: "open" })} loading={m.isPending}>
          Reopen
        </Button>
      )}
      {releaseDialog && (
        <PromptDialog
          title="Fixed in a specific version"
          label="Version"
          hint="The issue reopens only if a newer version has it. Version numbers compare numerically (1.10 is newer than 1.9); other names by when they were first seen."
          initial={issue.last_release ?? ""}
          onClose={() => setReleaseDialog(false)}
          onSubmit={(v) => go({ id: issue.issue_id, status: "resolved", resolved_in_release: v })}
        />
      )}
      {countDialog && (
        <PromptDialog
          title="Ignore until it happens more"
          label="Number of times"
          hint="The issue reopens once it has happened this many more times."
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
        <Button active={open} onClick={toggle} square={compact} icon={<UserCircle2 size={14} className={current ? "text-accent" : "text-ink-3"} />} title={current ? `Assigned to ${current}` : "Not assigned"}>
          {!compact && <span className="max-w-32 truncate">{current || "Assign"}</span>}
          {!compact && <ChevronDown size={13} className="text-ink-3" />}
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
            <Input autoFocus value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Name or e-mail, then Enter" aria-label="Assign to" />
          </form>
          {people.data?.me && people.data.me !== current && (
            <MenuItem icon={<UserCircle2 size={14} />} onClick={() => (assign.mutate({ id: issue.issue_id, assignee: "me" }), close())}>
              Assign to me ({people.data.me})
            </MenuItem>
          )}
          {suggestions.slice(0, 8).map((p) => (
            <MenuItem key={p} icon={<span className="inline-block w-[14px]" />} onClick={() => (assign.mutate({ id: issue.issue_id, assignee: p }), close())}>
              {p}
            </MenuItem>
          ))}
          {current && (
            <>
              <div className="my-1 h-px bg-border" />
              <MenuItem icon={<UserMinus size={14} />} onClick={() => (assign.mutate({ id: issue.issue_id, assignee: "" }), close())}>
                Remove {current}
              </MenuItem>
            </>
          )}
        </div>
      )}
    </Popover>
  );
}

/** Badges that explain a snoozed or version-resolved state, in plain words. */
export function StateQualifiers({ issue }: { issue: IssueSummary }) {
  const parts: React.ReactNode[] = [];
  if (issue.status === "ignored" && issue.snooze_until) parts.push(<Badge key="until" tone="outline">Ignored until {fmtWhen(issue.snooze_until)}</Badge>);
  if (issue.status === "ignored" && issue.snooze_count) {
    // snooze_count is the total the issue must reach, not the number still to come.
    const left = Math.max(0, issue.snooze_count - issue.occurrence_count);
    parts.push(
      <Badge key="count" tone="outline" title={`Reopens when it reaches ${issue.snooze_count.toLocaleString()} times in total`}>
        Ignored for {left.toLocaleString()} more {left === 1 ? "time" : "times"}
      </Badge>,
    );
  }
  if (issue.status === "resolved" && issue.resolved_in_release) parts.push(<Badge key="rel" tone="outline">Fixed in version {issue.resolved_in_release}</Badge>);
  if (parts.length === 0) return null;
  return <>{parts}</>;
}

/** The server's "until 2026-10-12T14:00:00Z or 140 occurrences" in words. */
export function snoozeWords(s: string): string {
  return s
    .replace(/\d{4}-\d{2}-\d{2}T[\d:.]+Z/g, (m) => fmtWhen(m))
    .replace(/(\d+) occurrences/, (_, n: string) => `it has happened ${Number(n).toLocaleString()} times`);
}

/** "ui" is the actor when sign-in is off, "system" is Pyloggle itself. */
export function actorName(actor: string): string {
  if (!actor) return "Someone";
  if (actor === "ui") return "Local user";
  if (actor === "system") return "Pyloggle";
  return actor;
}

const STATUS_VERB: Record<string, string> = { resolved: "resolved it", ignored: "ignored it", open: "reopened it" };

/** One activity entry as a sentence: "Ana resolved it", "It came back in version 1.4.1". */
export function activitySentence(a: IssueActivity): { who: string; what: string } {
  const who = actorName(a.actor);
  switch (a.kind) {
    case "status":
      return { who, what: STATUS_VERB[a.to ?? ""] ?? `changed the status to ${a.to}` };
    case "assign":
      return { who, what: a.to ? `assigned it to ${a.to}` : `removed ${a.from || "the assignee"}` };
    case "comment":
      return { who, what: "commented" };
    case "snooze":
      return { who, what: a.to ? `ignored it ${snoozeWords(a.to)}` : "ignored it for a while" };
    case "unsnooze":
      return { who: "Pyloggle", what: "reopened it: the ignore period ended" };
    case "regression":
      return { who: "Pyloggle", what: a.release ? `reopened it: it came back in version ${a.release}` : "reopened it: it came back after being resolved" };
    case "resolve_in_release":
      return { who, what: a.release ? `marked it fixed in version ${a.release}` : "marked it fixed in the next version" };
  }
  return { who, what: a.kind.replace(/_/g, " ") };
}

export function ActivityTab({ issueId }: { issueId: string }) {
  const q = useIssueActivity(issueId);
  const add = useAddComment();
  const [body, setBody] = useState("");
  return (
    <div className="space-y-4">
      <form
        className="flex flex-col gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (!body.trim()) return;
          add.mutate({ id: issueId, body: body.trim() }, { onSuccess: () => setBody("") });
        }}
      >
        <Textarea rows={2} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Leave a note for the team…" aria-label="Comment" />
        <div className="flex justify-end">
          <Button type="submit" variant="primary" icon={<MessageSquare size={14} />} loading={add.isPending} disabled={!body.trim()}>
            Comment
          </Button>
        </div>
      </form>
      <QueryState
        query={q}
        isEmpty={(d) => d.items.length === 0}
        skeleton={<Skeleton rows={4} />}
        empty={<EmptyState title="No activity yet" hint="Status changes, assignments, comments and returns after a fix show up here." className="py-6" />}
      >
        {(d) => (
          <ol className="relative ml-2 border-l border-border pl-5">
            {d.items.map((a) => (
              <ActivityRow key={a.id} a={a} />
            ))}
          </ol>
        )}
      </QueryState>
    </div>
  );
}

function ActivityRow({ a }: { a: IssueActivity }) {
  const tone = a.kind === "regression" ? "var(--st-critical)" : a.kind === "comment" ? "var(--accent)" : a.kind === "status" && a.to === "resolved" ? "var(--st-good)" : "var(--text-3)";
  const { who, what } = activitySentence(a);
  return (
    <li className="relative py-2">
      <span className="absolute -left-[25px] top-[13px] h-2.5 w-2.5 rounded-full border-2 border-surface" style={{ background: tone }} aria-hidden />
      <div className="flex flex-wrap items-baseline gap-x-1.5 text-[13px]">
        <span className="font-medium text-ink">{who}</span>
        <span className="text-ink-2">{what}</span>
        <span className="ml-auto text-xs text-ink-3" title={fmtTs(a.ts)}>
          <TimeAgo iso={a.ts} />
        </span>
      </div>
      {a.comment && a.kind !== "regression" && <div className="mt-1 whitespace-pre-wrap break-words rounded-md bg-surface-2 px-3 py-2 text-[13px] text-ink">{a.comment}</div>}
    </li>
  );
}
