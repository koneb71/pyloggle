import { useMemo, useState } from "react";
import { AlertTriangle, Eraser, Trash2, UserX } from "lucide-react";
import clsx from "clsx";
import { useProjectMutations, useProjectUsage } from "@/api/hooks";
import type { Project } from "@/api/types";
import { Sparkline } from "@/components/charts/sparkline";
import { Button, Dialog, ErrorState, Field, Input, Skeleton, toast } from "@/components/ui";
import { fmtBytes, fmtNumber, fmtPct } from "@/lib/time";
import { ClearLogsDialog } from "@/features/logs/clear-logs-dialog";

const SOURCE_LABEL: Record<string, string> = { logs: "Messages", errors: "Error details", spans: "Request steps", metrics: "Custom measurements" };

/** A small bar of storage used against the quota, for the projects table. */
export function UsageBar({ project }: { project: Project }) {
  const usage = useProjectUsage(project.id);
  const u = usage.data;
  if (usage.isPending) return <span className="skeleton inline-block h-3 w-24" aria-label="Loading" />;
  if (!u) return <span className="text-xs text-ink-3">Unknown</span>;
  if (u.quota_gb <= 0) return <span className="text-[13px] text-ink-2">{fmtBytes(u.stored_disk_est)}</span>;
  const pct = u.stored_pct;
  return (
    <span className="flex w-full flex-col gap-1" title={`${fmtBytes(u.stored_disk_est)} of ${u.quota_gb} GB`}>
      <span className="text-xs tabular text-ink-2">
        {fmtBytes(u.stored_disk_est)} <span className="text-ink-3">of {u.quota_gb} GB</span>
      </span>
      <span className="h-1.5 w-full overflow-hidden rounded-full bg-surface-3">
        <span className={clsx("block h-1.5 rounded-full", pct >= 90 ? "bg-critical" : pct >= 70 ? "bg-warning" : "bg-accent")} style={{ width: `${Math.min(100, Math.max(2, pct))}%` }} />
      </span>
    </span>
  );
}

/** How much one project stores, what it is, and how long it is kept. */
export function StoragePanel({ project }: { project: Project }) {
  const usage = useProjectUsage(project.id);
  const u = usage.data;
  const daily = useMemo(() => {
    if (!u) return [];
    const byDay = new Map<string, number>();
    for (const d of u.daily) byDay.set(d.day.slice(0, 10), (byDay.get(d.day.slice(0, 10)) ?? 0) + d.bytes_est);
    const out: number[] = [];
    const today = new Date();
    for (let i = 29; i >= 0; i--) out.push(byDay.get(new Date(today.getTime() - i * 864e5).toISOString().slice(0, 10)) ?? 0);
    return out;
  }, [u]);
  if (usage.isPending) return <Skeleton rows={4} className="p-5" />;
  if (usage.isError || !u) return <ErrorState error={usage.error} retry={() => usage.refetch()} title="Couldn't load storage" />;
  const pct = u.quota_gb > 0 ? u.stored_pct : 0;
  const sources = Object.entries(u.by_source).sort((a, b) => b[1].disk_est - a[1].disk_est);
  const r = u.retention;
  return (
    <div className="space-y-5 p-5 text-[13px]">
      {u.deleting && (
        <div className="flex items-center gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-warning-text">
          <AlertTriangle size={14} /> Data is still being deleted for this project.
        </div>
      )}
      <div>
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <span className="text-[15px] font-semibold text-ink">{fmtBytes(u.stored_disk_est)} stored</span>
          <span className="text-ink-2">{u.quota_gb > 0 ? `${fmtPct(pct)} of the ${u.quota_gb} GB limit` : "No storage limit"}</span>
        </div>
        {u.quota_gb > 0 && (
          <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-surface-3">
            <div className={clsx("h-2 rounded-full", pct >= 90 ? "bg-critical" : pct >= 70 ? "bg-warning" : "bg-accent")} style={{ width: `${Math.min(100, pct)}%` }} />
          </div>
        )}
        <p className="mt-1.5 text-xs text-ink-3">
          Over the last {u.window_days} days, after compression (about {fmtBytes(u.stored_bytes_est)} before).
          {u.quota_gb > 0 && " Above the limit, info and debug messages are dropped first; errors keep coming in."}
        </p>
      </div>
      <div>
        <div className="mb-1.5 flex items-baseline justify-between">
          <span className="font-medium text-ink">Received per day</span>
          <span className="text-xs text-ink-3">last 30 days</span>
        </div>
        <Sparkline values={daily} width={560} height={36} color="var(--accent)" title="Data received per day" />
      </div>
      <div>
        <div className="mb-1.5 font-medium text-ink">What is stored</div>
        <dl className="divide-y divide-border rounded-lg border border-border">
          {sources.length === 0 && <div className="px-3 py-2 text-ink-3">Nothing stored yet.</div>}
          {sources.map(([src, v]) => (
            <div key={src} className="flex items-center gap-3 px-3 py-2">
              <dt className="flex-1 text-ink">{SOURCE_LABEL[src] ?? src}</dt>
              <dd className="w-28 text-right tabular text-ink-3">{fmtNumber(v.rows)} rows</dd>
              <dd className="w-24 text-right font-medium tabular text-ink">{fmtBytes(v.disk_est)}</dd>
            </div>
          ))}
        </dl>
      </div>
      <div>
        <div className="mb-1.5 font-medium text-ink">How long data is kept</div>
        <p className="text-ink-2">
          {r.override_days > 0 ? `This project keeps everything for ${r.override_days} days. ` : ""}
          Messages {r.logs} days · error details {r.errors} days · request steps {r.spans} days · custom measurements {r.metrics} days.
        </p>
      </div>
    </div>
  );
}

/** Irreversible actions, kept apart and explained. */
export function DangerZone({ project, onDeleted }: { project: Project; onDeleted: () => void }) {
  const { remove, purgeUser } = useProjectMutations();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [purge, setPurge] = useState(false);
  const [clearLogs, setClearLogs] = useState(false);
  return (
    <div className="space-y-3 p-5">
      <div className="flex items-start justify-between gap-4 rounded-lg border border-border p-4">
        <div>
          <div className="text-[13px] font-medium text-ink">Erase one person's data</div>
          <p className="mt-0.5 text-[13px] text-ink-3">For privacy requests: removes the messages and error details recorded for one person.</p>
        </div>
        <Button variant="danger" icon={<UserX size={14} />} onClick={() => setPurge(true)} className="flex-none">
          Erase…
        </Button>
      </div>
      <div className="flex items-start justify-between gap-4 rounded-lg border border-border p-4">
        <div>
          <div className="text-[13px] font-medium text-ink">Clear all messages</div>
          <p className="mt-0.5 text-[13px] text-ink-3">Deletes every message on the Logs page, with the counts built from them. Issues, request counts, request timelines and measurements stay.</p>
        </div>
        <Button variant="danger" icon={<Eraser size={14} />} onClick={() => setClearLogs(true)} className="flex-none">
          Clear…
        </Button>
      </div>
      <div className="flex items-start justify-between gap-4 rounded-lg border border-critical/30 p-4">
        <div>
          <div className="text-[13px] font-medium text-ink">Delete this project</div>
          <p className="mt-0.5 text-[13px] text-ink-3">Removes all of its data, issues, alerts and saved searches. Its keys stop working at once.</p>
        </div>
        <Button variant="danger" icon={<Trash2 size={14} />} onClick={() => setConfirmDelete(true)} className="flex-none">
          Delete…
        </Button>
      </div>
      {confirmDelete && (
        <DeleteDialog
          project={project}
          pending={remove.isPending}
          error={remove.error}
          onClose={() => setConfirmDelete(false)}
          onConfirm={() => remove.mutate(project.id, { onSuccess: () => (setConfirmDelete(false), onDeleted()) })}
        />
      )}
      {clearLogs && <ClearLogsDialog project={project} onClose={() => setClearLogs(false)} />}
      {purge && (
        <PurgeUserDialog
          pending={purgeUser.isPending}
          error={purgeUser.error}
          onClose={() => setPurge(false)}
          onConfirm={(v) => purgeUser.mutate({ id: project.id, ...v }, { onSuccess: () => (setPurge(false), toast.success("Erasing started", "It finishes in the background within a few minutes.")) })}
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
        Every message, error, request, measurement, issue, alert and saved search of this project is removed, and its access keys stop working at once. This can't be undone, and the project id <span className="font-mono text-ink">{project.id}</span> can't be used again.
      </p>
      <Field label={`Type ${project.id} to confirm`} className="mt-3">
        <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus />
      </Field>
      {!!error && (
        <div role="alert" className="mt-2 text-[13px] text-critical-text">
          {String((error as Error).message ?? error)}
        </div>
      )}
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
    <Dialog open onClose={onClose} title="Erase one person's data">
      <p className="text-[13px] text-ink-2">Removes the messages and error details recorded for one person, for example after a privacy request. Issue totals keep their counts; requests and measurements don't record people.</p>
      <div className="mt-3 space-y-3">
        <Field label="Anonymized person id" hint="Shown as 'Person (anonymized)' in an error's User & system tab.">
          <Input value={hash} onChange={(e) => setHash(e.target.value)} className="font-mono" />
        </Field>
        <Field label="or their e-mail" hint="Only matches if your apps send e-mail addresses.">
          <Input value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
      </div>
      {!!error && (
        <div role="alert" className="mt-2 text-[13px] text-critical-text">
          {String((error as Error).message ?? error)}
        </div>
      )}
      <div className="mt-4 flex justify-end gap-2">
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="danger" disabled={!hash.trim() && !email.trim()} loading={pending} onClick={() => onConfirm({ user_id_hash: hash.trim() || undefined, user_email: email.trim() || undefined })}>
          Erase data
        </Button>
      </div>
    </Dialog>
  );
}
