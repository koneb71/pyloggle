import { useState } from "react";
import { KeyRound, Plus, ShieldOff } from "lucide-react";
import clsx from "clsx";
import { useProjectKeys, useProjectMutations } from "@/api/hooks";
import type { APIKey, Project } from "@/api/types";
import { Badge, Button, Checkbox, Dialog, Field, Input, Skeleton, TimeAgo } from "@/components/ui";

const SCOPE_HELP: Record<string, string> = {
  ingest: "send logs, errors, traces and metrics",
  read: "query logs, issues, traces and metrics",
  write: "change issue state, comments, saved views and alert rules",
};

/** The keys of one project: who can send, who can read, who can change things. */
export function ProjectKeysPanel({ project, canEdit, onCreated }: { project: Project; canEdit: boolean; onCreated: (key: APIKey, secret: string) => void }) {
  const keys = useProjectKeys(project.id, canEdit);
  const { revokeKey } = useProjectMutations();
  const [creating, setCreating] = useState(false);
  const [confirm, setConfirm] = useState<APIKey | null>(null);
  if (!canEdit) return null;
  if (keys.isPending) return <Skeleton rows={2} className="p-3" />;
  if (keys.isError) return <div className="px-3 py-2 text-xs text-critical">Keys unavailable: {String(keys.error)}</div>;
  const items = keys.data?.items ?? [];
  const active = items.filter((k) => !k.revoked_at);
  const revoked = items.filter((k) => k.revoked_at);
  return (
    <div className="space-y-2 border-t border-border bg-surface-2/40 px-3 py-3 text-xs">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-[13px] font-medium text-ink">
          <KeyRound size={13} className="text-ink-3" /> API keys
          <span className="font-normal text-ink-3">
            · {active.length} active{revoked.length > 0 && `, ${revoked.length} revoked`}
          </span>
        </div>
        <Button size="xs" variant="primary" icon={<Plus size={12} />} onClick={() => setCreating(true)}>
          New key
        </Button>
      </div>
      {items.length === 0 ? (
        <div className="text-ink-3">No keys yet. Create one to send data or to query this project from scripts.</div>
      ) : (
        <table className="w-full">
          <thead className="text-left text-[10px] uppercase tracking-wide text-ink-3">
            <tr>
              <th className="py-1 font-medium">name</th>
              <th className="py-1 font-medium">key</th>
              <th className="py-1 font-medium">scopes</th>
              <th className="py-1 font-medium">created</th>
              <th className="py-1 font-medium">last used</th>
              <th className="py-1" />
            </tr>
          </thead>
          <tbody>
            {[...active, ...revoked].map((k) => (
              <tr key={k.id} className={clsx("border-t border-border/60", k.revoked_at && "text-ink-3")}>
                <td className="py-1.5 pr-2">
                  <span className={clsx("font-medium", k.revoked_at ? "line-through" : "text-ink")}>{k.name}</span>
                  {k.revoked_at && (
                    <span className="ml-1.5 text-[11px]">
                      revoked <TimeAgo iso={k.revoked_at} />
                      {k.revoked_by && ` by ${k.revoked_by}`}
                    </span>
                  )}
                </td>
                <td className="py-1.5 pr-2 font-mono text-[11px]">{k.key_prefix ? `${k.key_prefix}…` : "0.2 key"}</td>
                <td className="py-1.5 pr-2">
                  <div className="flex flex-wrap gap-1">
                    {k.scopes.map((s) => (
                      <Badge key={s} tone={k.revoked_at ? "neutral" : "outline"} title={SCOPE_HELP[s]}>
                        {s}
                      </Badge>
                    ))}
                  </div>
                </td>
                <td className="py-1.5 pr-2 whitespace-nowrap">
                  <TimeAgo iso={k.created_at} />
                  {k.created_by && k.created_by !== "migration" && <span className="text-ink-3"> by {k.created_by}</span>}
                </td>
                <td className="py-1.5 pr-2 whitespace-nowrap">{k.last_used_at ? <TimeAgo iso={k.last_used_at} /> : <span className="text-ink-3">never</span>}</td>
                <td className="py-1.5 text-right">
                  {!k.revoked_at && (
                    <Button size="xs" variant="ghost" icon={<ShieldOff size={12} />} aria-label={`Revoke ${k.name}`} title="Revoke key" onClick={() => setConfirm(k)} />
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {creating && (
        <NewKeyDialog
          project={project}
          scopes={keys.data?.scopes ?? ["ingest", "read", "write"]}
          onClose={() => setCreating(false)}
          onCreated={(key, secret) => (onCreated(key, secret), setCreating(false))}
        />
      )}
      {confirm && (
        <Dialog open onClose={() => setConfirm(null)} title={`Revoke "${confirm.name}"?`}>
          <p className="text-[13px] text-ink-2">
            Requests with this key fail from the next one. Every SDK, exporter or script using <code className="rounded bg-surface-2 px-1 font-mono text-xs">{confirm.key_prefix || "it"}…</code> must switch to another key first.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button onClick={() => setConfirm(null)}>Cancel</Button>
            <Button
              variant="danger"
              loading={revokeKey.isPending}
              onClick={() => revokeKey.mutate({ id: project.id, key_id: confirm.id }, { onSuccess: () => setConfirm(null) })}
            >
              Revoke key
            </Button>
          </div>
        </Dialog>
      )}
    </div>
  );
}

function NewKeyDialog({ project, scopes, onClose, onCreated }: { project: Project; scopes: string[]; onClose: () => void; onCreated: (key: APIKey, secret: string) => void }) {
  const { createKey } = useProjectMutations();
  const [name, setName] = useState("");
  const [picked, setPicked] = useState<string[]>(["ingest", "read"]);
  const toggle = (s: string) => setPicked((p) => (p.includes(s) ? p.filter((x) => x !== s) : [...p, s]));
  return (
    <Dialog open onClose={onClose} title={`New API key for ${project.name}`}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          createKey.mutate({ id: project.id, name: name.trim(), scopes: picked }, { onSuccess: (r) => onCreated(r.key, r.api_key) });
        }}
      >
        <Field label="Name" hint="where the key lives: an app, a CI job, a dashboard">
          <Input required autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="checkout-api (production)" maxLength={100} />
        </Field>
        <fieldset>
          <legend className="mb-1 block text-xs font-medium text-ink-2">Scopes</legend>
          <div className="space-y-1.5">
            {scopes.map((s) => (
              <Checkbox key={s} checked={picked.includes(s)} onChange={() => toggle(s)} label={<span>{s} <span className="text-ink-3">· {SCOPE_HELP[s]}</span></span>} />
            ))}
          </div>
          <div className="mt-1 text-[11px] text-ink-3">An SDK or an OpenTelemetry exporter needs only ingest. A read-only key is what you hand to a script or a dashboard.</div>
        </fieldset>
        {createKey.isError && <div className="text-xs text-critical">{(createKey.error as Error).message}</div>}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" loading={createKey.isPending} disabled={picked.length === 0 || !name.trim()}>
            Create key
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
