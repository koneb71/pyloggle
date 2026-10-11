import { useState } from "react";
import { useLocation } from "react-router";
import { Check, KeyRound, Pencil, Plus, RefreshCw } from "lucide-react";
import { useProjectMutations, useProjects } from "@/api/hooks";
import type { Me, Project } from "@/api/types";
import { PageHeader } from "@/components/shell/app-shell";
import { Badge, Button, Card, CardHeader, CopyButton, Dialog, EmptyState, ErrorState, Field, Input, Skeleton, TimeAgo } from "@/components/ui";

export function ProjectsPage({ me }: { me: Me }) {
  const projects = useProjects();
  const { select, rotate } = useProjectMutations();
  const [editing, setEditing] = useState<Partial<Project> | null>(null);
  const [revealed, setRevealed] = useState<{ id: string; key: string; action: string } | null>(null);
  const [confirmRotate, setConfirmRotate] = useState<Project | null>(null);
  const first = new URLSearchParams(useLocation().search).get("first") === "1";
  const canEdit = me.is_admin;
  const items = projects.data?.items ?? [];

  return (
    <>
      <PageHeader title="Projects" sub="Each project has its own API key, quota and data">
        {canEdit && (
          <Button variant="primary" icon={<Plus size={13} />} onClick={() => setEditing({})}>
            New project
          </Button>
        )}
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[1100px] flex-col gap-3 p-4">
          {revealed && (
            <Card className="border-good/50 p-3">
              <div className="flex items-start gap-3">
                <KeyRound size={16} className="mt-0.5 text-good" />
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-ink">API key {revealed.action} for {revealed.id}</div>
                  <div className="text-xs text-ink-3">Copy it now: it is shown once and only its hash is stored.</div>
                  <div className="mt-2 flex items-center gap-2">
                    <code className="select-all break-all rounded bg-surface-2 px-2 py-1 font-mono text-xs text-ink">{revealed.key}</code>
                    <CopyButton text={revealed.key} size="sm" />
                  </div>
                  <pre className="mt-2 overflow-x-auto rounded bg-surface-2 p-2 font-mono text-[11px] text-ink-2">{`PYLOGGLE_INGEST_URL=${me.features.public_url || window.location.origin}\nPYLOGGLE_API_KEY=${revealed.key}\nPYLOGGLE_PROJECT_ID=${revealed.id}`}</pre>
                </div>
                <Button size="xs" variant="ghost" onClick={() => setRevealed(null)}>
                  Dismiss
                </Button>
              </div>
            </Card>
          )}
          {projects.isError && <ErrorState error={projects.error} retry={() => projects.refetch()} />}
          {projects.isPending ? (
            <Skeleton rows={4} className="p-3" />
          ) : items.length === 0 ? (
            <Card>
              <EmptyState title={first ? "Welcome — create your first project" : "No projects"} hint="A project is a tenant: one API key, one quota, one set of issues and logs." action={canEdit ? <Button variant="primary" onClick={() => setEditing({})}>Create project</Button> : <span className="text-xs text-ink-3">Ask a PYLOGGLE_UI_ADMINS member to create one.</span>} />
            </Card>
          ) : (
            <Card>
              <CardHeader title="All projects" sub={`${items.length} total`} />
              <ul className="divide-y divide-border">
                {items.map((p) => {
                  const active = me.project?.id === p.id;
                  return (
                    <li key={p.id} className="flex items-start gap-3 px-3 py-2.5">
                      <span className="flex h-7 w-7 flex-none items-center justify-center rounded bg-surface-3 text-xs font-semibold uppercase text-ink-2">{p.name.slice(0, 1)}</span>
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-medium text-ink">{p.name}</span>
                          <span className="font-mono text-xs text-ink-3">{p.id}</span>
                          {active && (
                            <Badge tone="accent">
                              <Check size={10} /> active
                            </Badge>
                          )}
                        </div>
                        {p.description && <div className="text-xs text-ink-2">{p.description}</div>}
                        <div className="mt-0.5 text-[11px] text-ink-3">
                          {p.max_events_per_second ? `${p.max_events_per_second} events/s` : "no rate limit"} · {p.max_storage_gb ? `${p.max_storage_gb} GB` : "no storage cap"} · key created <TimeAgo iso={p.api_key_created_at} /> · created <TimeAgo iso={p.created_at} />
                        </div>
                      </div>
                      <div className="flex flex-none items-center gap-1">
                        {!active && (
                          <Button size="xs" onClick={() => select.mutate(p.id)} loading={select.isPending && select.variables === p.id}>
                            Switch to
                          </Button>
                        )}
                        {canEdit && <Button size="xs" variant="ghost" icon={<Pencil size={12} />} aria-label={`Edit ${p.name}`} onClick={() => setEditing(p)} />}
                        {canEdit && <Button size="xs" variant="ghost" icon={<RefreshCw size={12} />} aria-label={`Rotate key for ${p.name}`} title="Rotate API key" onClick={() => setConfirmRotate(p)} />}
                      </div>
                    </li>
                  );
                })}
              </ul>
            </Card>
          )}
          <Card className="p-3 text-xs text-ink-2">
            <div className="mb-1 font-medium text-ink">Send data</div>
            <div>
              Python: <code className="rounded bg-surface-2 px-1 font-mono">pip install pyloggle</code> then <code className="rounded bg-surface-2 px-1 font-mono">pyloggle.init()</code> with the env vars above. OpenTelemetry: point an OTLP/HTTP exporter at <code className="rounded bg-surface-2 px-1 font-mono">{(me.features.public_url || window.location.origin) + "/v1/logs"}</code> with header <code className="rounded bg-surface-2 px-1 font-mono">X-API-Key</code>.
            </div>
          </Card>
        </div>
      </div>
      {editing && <ProjectDialog project={editing} onClose={() => setEditing(null)} onCreated={(id, key) => setRevealed({ id, key, action: "created" })} />}
      {confirmRotate && (
        <Dialog open onClose={() => setConfirmRotate(null)} title={`Rotate API key for ${confirmRotate.name}?`}>
          <p className="text-[13px] text-ink-2">The current key stops working immediately. Every SDK and exporter using it must be updated with the new key.</p>
          <div className="mt-4 flex justify-end gap-2">
            <Button onClick={() => setConfirmRotate(null)}>Cancel</Button>
            <Button
              variant="danger"
              loading={rotate.isPending}
              onClick={() =>
                rotate.mutate(confirmRotate.id, {
                  onSuccess: (r) => {
                    setRevealed({ id: confirmRotate.id, key: r.api_key, action: "rotated" });
                    setConfirmRotate(null);
                  },
                })
              }
            >
              Rotate key
            </Button>
          </div>
        </Dialog>
      )}
    </>
  );
}

function ProjectDialog({ project, onClose, onCreated }: { project: Partial<Project>; onClose: () => void; onCreated: (id: string, key: string) => void }) {
  const { create, update } = useProjectMutations();
  const isNew = !project.id;
  const [p, setP] = useState<Partial<Project>>({ max_events_per_second: 0, max_storage_gb: 0, ...project });
  const upd = (patch: Partial<Project>) => setP((x) => ({ ...x, ...patch }));
  const m = isNew ? create : update;
  return (
    <Dialog open onClose={onClose} title={isNew ? "New project" : `Edit ${project.name}`}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          const body = { ...p, max_events_per_second: Number(p.max_events_per_second) || 0, max_storage_gb: Number(p.max_storage_gb) || 0 };
          if (isNew) create.mutate(body, { onSuccess: (r) => (onCreated(r.project.id, r.api_key), onClose()) });
          else update.mutate({ ...body, id: p.id! }, { onSuccess: onClose });
        }}
      >
        <Field label="Project id" hint="lowercase letters, digits, dashes; used in PYLOGGLE_PROJECT_ID">
          <Input required disabled={!isNew} value={p.id ?? ""} onChange={(e) => upd({ id: e.target.value.toLowerCase().replace(/[^a-z0-9-_]/g, "-") })} placeholder="my-service" />
        </Field>
        <Field label="Name">
          <Input required value={p.name ?? ""} onChange={(e) => upd({ name: e.target.value })} />
        </Field>
        <Field label="Description">
          <Input value={p.description ?? ""} onChange={(e) => upd({ description: e.target.value })} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Max events / second" hint="0 = unlimited">
            <Input type="number" min={0} value={p.max_events_per_second ?? 0} onChange={(e) => upd({ max_events_per_second: Number(e.target.value) })} />
          </Field>
          <Field label="Max storage (GB)" hint="0 = unlimited">
            <Input type="number" min={0} value={p.max_storage_gb ?? 0} onChange={(e) => upd({ max_storage_gb: Number(e.target.value) })} />
          </Field>
        </div>
        {m.isError && <div className="text-xs text-critical">{(m.error as Error).message}</div>}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" loading={m.isPending}>
            {isNew ? "Create project" : "Save"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
