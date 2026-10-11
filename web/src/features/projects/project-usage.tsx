import { useMemo, useState } from "react";
import { AlertTriangle, Trash2, UserX } from "lucide-react";
import clsx from "clsx";
import { useProjectMutations, useProjectUsage } from "@/api/hooks";
import type { Project } from "@/api/types";
import { Sparkline } from "@/components/charts/sparkline";
import { Button, Dialog, Field, Input, Skeleton } from "@/components/ui";
import { fmtBytes, fmtNumber, fmtPct } from "@/lib/time";

const SOURCE_LABEL: Record<string, string> = { logs: "Log lines", errors: "Error occurrences", spans: "Spans", metrics: "Metric points" };

/** Storage of one project against its quota, with the admin danger zone. */
export function ProjectUsagePanel({ project, canEdit, onDeleted }: { project: Project; canEdit: boolean; onDeleted: () => void }) {
  const usage = useProjectUsage(project.id);
  const { remove, purgeUser } = useProjectMutations();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [purge, setPurge] = useState(false);
  const u = usage.data;
  const daily = useMemo(() => {
    if (!u) return [];
    const byDay = new Map<string, number>();
    for (const d of u.daily) byDay.set(d.day, (byDay.get(d.day) ?? 0) + d.bytes_est);
    const out: number[] = [];
    const today = new Date();
    for (let i = 29; i >= 0; i--) {
      const d = new Date(today.getTime() - i * 864e5).toISOString().slice(0, 10);
      out.push([...byDay.entries()].find(([k]) => k.startsWith(d))?.[1] ?? 0);
    }
    return out;
  }, [u]);
  if (usage.isPending) return <Skeleton rows={3} className="p-3" />;
  if (usage.isError || !u) return <div className="px-3 py-2 text-xs text-critical">Usage unavailable: {String(usage.error)}</div>;
  const pct = u.quota_gb > 0 ? u.stored_pct : 0;
  const tone = pct >= 90 ? "bg-critical" : pct >= 70 ? "bg-serious" : "bg-accent";
  const sources = Object.entries(u.by_source).sort((a, b) => b[1].disk_est - a[1].disk_est);
  return (
    <div className="space-y-3 border-t border-border bg-surface-2/40 px-3 py-3 text-xs">
      {u.deleting && (
        <div className="flex items-center gap-2 rounded-md border border-serious/40 px-2 py-1.5 text-serious">
          <AlertTriangle size={13} /> A purge is still running for this project.
        </div>
      )}
      <div className="grid gap-3 md:grid-cols-[1fr_auto]">
        <div>
          <div className="flex items-baseline justify-between">
            <span className="text-[13px] font-medium text-ink">
              {fmtBytes(u.stored_disk_est)} on disk <span className="font-normal text-ink-3">(≈{fmtBytes(u.stored_bytes_est)} raw) over the last {u.window_days} days</span>
            </span>
            <span className="tabular text-ink-2">{u.quota_gb > 0 ? `${fmtPct(pct)} of ${u.quota_gb} GB` : "no quota"}</span>
          </div>
          <div className="mt-1 h-2 w-full overflow-hidden rounded bg-surface-3">
            <div className={clsx("h-2 rounded", tone)} style={{ width: `${Math.min(100, Math.max(u.quota_gb > 0 ? pct : 0, 0))}%` }} />
          </div>
          <div className="mt-1 text-[11px] text-ink-3">
            Retention: logs {u.retention.logs}d · errors {u.retention.errors}d · spans {u.retention.spans}d · metrics {u.retention.metrics}d
            {u.retention.override_days > 0 && <span className="text-ink-2"> · this project keeps {u.retention.override_days}d</span>}
            {u.quota_gb > 0 && <span> · over quota, info/debug lines are dropped; errors keep flowing up to 2×</span>}
          </div>
        </div>
        <div className="text-right">
          <div className="text-[10px] uppercase tracking-wide text-ink-3">30 days, raw bytes/day</div>
          <Sparkline values={daily} width={160} height={28} color="var(--accent)" title="Daily ingest" />
        </div>
      </div>
      <table className="w-full">
        <thead className="text-left text-[10px] uppercase tracking-wide text-ink-3">
          <tr>
            <th className="py-1 font-medium">source</th>
            <th className="py-1 text-right font-medium">rows</th>
            <th className="py-1 text-right font-medium">raw est.</th>
            <th className="py-1 text-right font-medium">on disk est.</th>
          </tr>
        </thead>
        <tbody>
          {sources.length === 0 && (
            <tr>
              <td colSpan={4} className="py-1 text-ink-3">
                nothing stored in the window
              </td>
            </tr>
          )}
          {sources.map(([src, v]) => (
            <tr key={src} className="border-t border-border">
              <td className="py-1 text-ink">{SOURCE_LABEL[src] ?? src}</td>
              <td className="py-1 text-right tabular text-ink-2">{fmtNumber(v.rows)}</td>
              <td className="py-1 text-right tabular text-ink-2">{fmtBytes(v.bytes_est)}</td>
              <td className="py-1 text-right tabular text-ink">{fmtBytes(v.disk_est)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {canEdit && (
        <div className="flex flex-wrap items-center gap-2 border-t border-border pt-2">
          <span className="text-[11px] font-medium uppercase tracking-wide text-ink-3">Danger zone</span>
          <Button size="xs" variant="danger" icon={<UserX size={12} />} onClick={() => setPurge(true)}>
            Purge a user
          </Button>
          <Button size="xs" variant="danger" icon={<Trash2 size={12} />} onClick={() => setConfirmDelete(true)}>
            Delete project
          </Button>
        </div>
      )}
      {confirmDelete && (
        <DeleteDialog
          project={project}
          pending={remove.isPending}
          error={remove.error}
          onClose={() => setConfirmDelete(false)}
          onConfirm={() => remove.mutate(project.id, { onSuccess: () => (setConfirmDelete(false), onDeleted()) })}
        />
      )}
      {purge && (
        <PurgeUserDialog
          pending={purgeUser.isPending}
          error={purgeUser.error}
          onClose={() => setPurge(false)}
          onConfirm={(v) => purgeUser.mutate({ id: project.id, ...v }, { onSuccess: () => (setPurge(false), usage.refetch()) })}
        />
      )}
    </div>
  );
}

function DeleteDialog({ project, pending, error, onClose, onConfirm }: { project: Project; pending: boolean; error: unknown; onClose: () => void; onConfirm: () => void }) {
  const [typed, setTyped] = useState("");
  return (
    <Dialog open onClose={onClose} title={`Delete ${project.name}?`}>
      <p className="text-[13px] text-ink-2">
        Every log line, error, span, metric, issue, alert rule and saved view of <span className="font-mono text-ink">{project.id}</span> is removed and the API key stops working immediately. The id cannot be reused.
      </p>
      <Field label={`Type ${project.id} to confirm`} className="mt-3">
        <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus />
      </Field>
      {!!error && <div className="mt-2 text-xs text-critical">{String((error as Error).message ?? error)}</div>}
      <div className="mt-4 flex justify-end gap-2">
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="danger" disabled={typed !== project.id} loading={pending} onClick={onConfirm}>
          Delete project
        </Button>
      </div>
    </Dialog>
  );
}

function PurgeUserDialog({ pending, error, onClose, onConfirm }: { pending: boolean; error: unknown; onClose: () => void; onConfirm: (v: { user_id_hash?: string; user_email?: string }) => void }) {
  const [hash, setHash] = useState("");
  const [email, setEmail] = useState("");
  return (
    <Dialog open onClose={onClose} title="Purge a user's data">
      <p className="text-[13px] text-ink-2">Removes the log lines and error occurrences of one user (data-subject requests). Issue counters keep their totals; spans and metrics carry no user.</p>
      <div className="mt-3 space-y-3">
        <Field label="User id hash" hint="the user_id_hash the SDK sends (see any occurrence's User block)">
          <Input value={hash} onChange={(e) => setHash(e.target.value)} className="font-mono" />
        </Field>
        <Field label="or e-mail" hint="matches occurrences stored with send_default_pii">
          <Input value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
      </div>
      {!!error && <div className="mt-2 text-xs text-critical">{String((error as Error).message ?? error)}</div>}
      <div className="mt-4 flex justify-end gap-2">
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="danger" disabled={!hash.trim() && !email.trim()} loading={pending} onClick={() => onConfirm({ user_id_hash: hash.trim() || undefined, user_email: email.trim() || undefined })}>
          Purge
        </Button>
      </div>
    </Dialog>
  );
}
