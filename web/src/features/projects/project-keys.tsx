import { useState } from "react";
import { KeyRound, Plus } from "lucide-react";
import clsx from "clsx";
import { useProjectKeys, useProjectMutations } from "@/api/hooks";
import type { APIKey, Project } from "@/api/types";
import { Badge, Button, Checkbox, Dialog, EmptyState, ErrorState, Field, Input, Skeleton, TimeAgo } from "@/components/ui";
import { SCOPE_WORDS, scopeLabel } from "./project-words";

/** The access keys of one project: who can send data, who can look, who can change things. */
export function ProjectKeysPanel({ project, onCreated }: { project: Project; onCreated: (key: APIKey, secret: string) => void }) {
  const keys = useProjectKeys(project.id);
  const { revokeKey } = useProjectMutations();
  const [creating, setCreating] = useState(false);
  const [confirm, setConfirm] = useState<APIKey | null>(null);
  if (keys.isPending) return <Skeleton rows={3} className="p-5" />;
  if (keys.isError) return <ErrorState error={keys.error} retry={() => keys.refetch()} title="Couldn't load the access keys" />;
  const items = keys.data?.items ?? [];
  const active = items.filter((k) => !k.revoked_at);
  const disabled = items.filter((k) => k.revoked_at);
  return (
    <div className="space-y-4 p-5">
      <div className="flex items-start justify-between gap-3">
        <p className="text-[13px] text-ink-2">Apps and scripts use an access key to send data to this project or read from it. Give each app its own key, so you can disable one without touching the others.</p>
        <Button variant="primary" icon={<Plus size={14} />} onClick={() => setCreating(true)} className="flex-none">
          New key
        </Button>
      </div>
      {items.length === 0 ? (
        <EmptyState icon={<KeyRound size={18} />} title="No access keys yet" hint="Create one to connect an app or a script." className="py-8" />
      ) : (
        <ul className="divide-y divide-border rounded-lg border border-border">
          {[...active, ...disabled].map((k) => (
            <li key={k.id} className={clsx("flex items-start gap-3 px-4 py-3", k.revoked_at && "bg-surface-2/60")}>
              <KeyRound size={15} className={clsx("mt-0.5 flex-none", k.revoked_at ? "text-ink-3" : "text-accent")} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className={clsx("text-[13px] font-medium", k.revoked_at ? "text-ink-3 line-through" : "text-ink")}>{k.name}</span>
                  {k.key_prefix ? (
                    <code className="font-mono text-[11px] text-ink-3">{k.key_prefix}…</code>
                  ) : (
                    <Badge tone="outline" title="Created before keys showed their first characters">
                      Older key
                    </Badge>
                  )}
                  {k.revoked_at && <Badge>Disabled</Badge>}
                </div>
                <div className="mt-1 flex flex-wrap gap-1">
                  {k.scopes.map((s) => (
                    <Badge key={s} tone={k.revoked_at ? "neutral" : "accent"} title={`${SCOPE_WORDS[s]?.explain ?? s} (${s})`}>
                      {scopeLabel(s)}
                    </Badge>
                  ))}
                </div>
                <div className="mt-1 text-xs text-ink-3">
                  {k.revoked_at ? (
                    <>
                      Disabled <TimeAgo iso={k.revoked_at} />
                      {k.revoked_by && ` by ${k.revoked_by}`}
                    </>
                  ) : (
                    <>
                      Created <TimeAgo iso={k.created_at} />
                      {k.created_by && k.created_by !== "migration" && ` by ${k.created_by}`} · {k.last_used_at ? <>last used <TimeAgo iso={k.last_used_at} /></> : "never used"}
                    </>
                  )}
                </div>
              </div>
              {!k.revoked_at && (
                <Button size="xs" variant="danger" onClick={() => setConfirm(k)}>
                  Disable key
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {creating && <NewKeyDialog project={project} scopes={keys.data?.scopes ?? ["ingest", "read", "write"]} onClose={() => setCreating(false)} onCreated={(key, secret) => (onCreated(key, secret), setCreating(false))} />}
      {confirm && (
        <Dialog open onClose={() => setConfirm(null)} title={`Disable "${confirm.name}"?`}>
          <p className="text-[13px] text-ink-2">
            Anything still using this key{confirm.key_prefix && <> (it starts with <code className="rounded bg-surface-2 px-1 font-mono text-xs">{confirm.key_prefix}</code>)</>} stops working right away. Move those apps and scripts to another key first. This can't be undone.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button onClick={() => setConfirm(null)}>Cancel</Button>
            <Button variant="danger" loading={revokeKey.isPending} onClick={() => revokeKey.mutate({ id: project.id, key_id: confirm.id }, { onSuccess: () => setConfirm(null) })}>
              Disable key
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
  const [picked, setPicked] = useState<string[]>(["ingest"]);
  const toggle = (s: string) => setPicked((p) => (p.includes(s) ? p.filter((x) => x !== s) : [...p, s]));
  return (
    <Dialog open onClose={onClose} title={`New access key for ${project.name}`}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          createKey.mutate({ id: project.id, name: name.trim(), scopes: picked }, { onSuccess: (r) => onCreated(r.key, r.api_key) });
        }}
      >
        <Field label="Name" hint="Where the key will be used: an app, a CI job, a dashboard.">
          <Input required autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="checkout-api (production)" maxLength={100} />
        </Field>
        <fieldset>
          <legend className="mb-2 block text-[13px] font-medium text-ink">What it can do</legend>
          <div className="space-y-2">
            {scopes.map((s) => (
              <Checkbox
                key={s}
                checked={picked.includes(s)}
                onChange={() => toggle(s)}
                label={
                  <span>
                    <span className="font-medium">{scopeLabel(s)}</span> <span className="text-ink-3">· {SCOPE_WORDS[s]?.explain ?? s}</span>
                  </span>
                }
              />
            ))}
          </div>
          <p className="mt-2 text-xs text-ink-3">An app or an OpenTelemetry exporter needs only "Send data". Give scripts and dashboards "View".</p>
        </fieldset>
        {createKey.isError && (
          <div role="alert" className="text-[13px] text-critical-text">
            {(createKey.error as Error).message}
          </div>
        )}
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
