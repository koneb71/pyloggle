import { useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { KeyRound, Pencil, Plus, Settings2 } from "lucide-react";
import { useAudit, useOverview, useProjectMutations, useProjects } from "@/api/hooks";
import type { APIKey, Me, Project } from "@/api/types";
import { Badge, Button, Card, CopyButton, DataTable, Dialog, Drawer, EmptyState, Field, Input, Page, QueryState, Section, Skeleton, Tabs, TimeAgo, type Column } from "@/components/ui";
import { fmtWhen } from "@/lib/time";
import { ProjectKeysPanel } from "./project-keys";
import { DangerZone, StoragePanel, UsageBar } from "./project-usage";
import { ConnectGuide } from "./connect-guide";
import { auditWords, limitsWords, scopeLabel } from "./project-words";

interface Revealed {
  projectId: string;
  key: string;
  name?: string;
  scopes?: string[];
}

export function ProjectsPage({ me }: { me: Me }) {
  const projects = useProjects();
  const { select } = useProjectMutations();
  const nav = useNavigate();
  const [editing, setEditing] = useState<Partial<Project> | null>(null);
  const [revealed, setRevealed] = useState<Revealed | null>(null);
  const [drawer, setDrawer] = useState<{ id: string; tab: string } | null>(null);
  const first = new URLSearchParams(useLocation().search).get("first") === "1";
  const isAdmin = me.is_admin;
  const audit = useAudit(isAdmin);
  const recent = useOverview({ from: "-7d" });
  const items = projects.data?.items ?? [];
  const drawerProject = items.find((p) => p.id === drawer?.id);
  const serverUrl = me.features.public_url || window.location.origin;
  const current = me.project;
  const empty = !!current && recent.data?.overview.events === 0;
  const guideKey = revealed && revealed.projectId === current?.id && (!revealed.scopes || revealed.scopes.includes("ingest")) ? revealed.key : undefined;

  const columns: Column<Project>[] = [
    {
      id: "name",
      header: "Project",
      cell: (p) => (
        <div className="flex min-w-0 items-center gap-3">
          <span className="flex h-8 w-8 flex-none items-center justify-center rounded-md bg-surface-3 text-[13px] font-semibold uppercase text-ink-2">{p.name.slice(0, 1)}</span>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate font-medium text-ink">{p.name}</span>
              {current?.id === p.id && <Badge tone="accent">Current</Badge>}
            </div>
            <div className="truncate text-xs text-ink-3">{p.description || <span className="font-mono text-[11px]">{p.id}</span>}</div>
          </div>
        </div>
      ),
    },
    { id: "limits", header: "Limits", width: "minmax(180px,1fr)", cell: (p) => <span className="text-[13px] text-ink-2">{limitsWords(p)}</span> },
    ...(isAdmin ? [{ id: "storage", header: "Storage used", width: "160px", cell: (p: Project) => <UsageBar project={p} /> }] : []),
    { id: "created", header: "Created", width: "120px", cell: (p) => <TimeAgo iso={p.created_at} className="text-ink-3" /> },
    {
      id: "actions",
      header: <span className="sr-only">Actions</span>,
      width: isAdmin ? "176px" : "84px",
      align: "right",
      cell: (p) => (
        <div className="flex items-center gap-1.5">
          {current?.id !== p.id && (
            <Button size="xs" onClick={() => select.mutate(p.id, { onSuccess: () => nav("/overview") })} loading={select.isPending && select.variables === p.id}>
              Open
            </Button>
          )}
          {isAdmin && (
            <Button size="xs" variant="ghost" icon={<Settings2 size={13} />} onClick={() => setDrawer({ id: p.id, tab: "keys" })}>
              Settings
            </Button>
          )}
        </div>
      ),
    },
  ];

  return (
    <Page
      title="Projects"
      description="A project keeps one team's or app's data separate, with its own access keys and storage limits."
      actions={
        isAdmin && (
          <Button variant="primary" icon={<Plus size={14} />} onClick={() => setEditing({})}>
            New project
          </Button>
        )
      }
    >
      {revealed && <NewKeyCard revealed={revealed} serverUrl={serverUrl} onDismiss={() => setRevealed(null)} />}
      {current && empty && <ConnectGuide serverUrl={serverUrl} projectId={current.id} apiKey={guideKey} prominent />}

      <Section title="All projects" description={projects.data ? `${items.length === 1 ? "1 project" : `${items.length} projects`}. Open one to see its data.` : undefined} flush>
        <QueryState
          query={projects}
          isEmpty={(d) => d.items.length === 0}
          skeleton={<Skeleton rows={4} className="p-4" />}
          empty={
            <EmptyState
              title={first ? "Welcome! Create your first project" : "No projects yet"}
              hint="A project holds the data of one app or team, with its own access keys."
              action={
                isAdmin ? (
                  <Button variant="primary" onClick={() => setEditing({})}>
                    Create a project
                  </Button>
                ) : (
                  <span className="text-[13px] text-ink-3">Ask an administrator to create one.</span>
                )
              }
            />
          }
        >
          {(d) => <DataTable label="Projects" columns={columns} rows={d.items} rowKey={(p) => p.id} stickyHeader={false} />}
        </QueryState>
      </Section>

      {current && !empty && <ConnectGuide serverUrl={serverUrl} projectId={current.id} apiKey={guideKey} />}

      {isAdmin && (
        <Section title="Admin activity" description="Who changed projects, keys and data, newest first." flush>
          <QueryState query={audit} compact isEmpty={(d) => d.items.length === 0} skeleton={<Skeleton rows={3} className="p-4" />} empty={<p className="px-4 py-3 text-[13px] text-ink-3">Nothing yet.</p>}>
            {(d) => (
              <ul className="divide-y divide-border">
                {d.items.map((a, i) => (
                  <li key={i} className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 px-4 py-2.5 text-[13px]">
                    <span className="font-medium text-ink">{a.actor === "ui" ? "Local user" : a.actor}</span>
                    <span className="text-ink-2">{auditWords(a)}</span>
                    {a.project_id && <span className="text-ink-3">in {items.find((p) => p.id === a.project_id)?.name ?? a.project_id}</span>}
                    {a.target && <span className="font-mono text-[11px] text-ink-3">{a.target}</span>}
                    {a.details && <span className="text-xs text-ink-3">· {a.details}</span>}
                    <span className="ml-auto text-xs tabular text-ink-3">{fmtWhen(a.ts)}</span>
                  </li>
                ))}
              </ul>
            )}
          </QueryState>
        </Section>
      )}

      {drawerProject && (
        <Drawer
          open
          onClose={() => setDrawer(null)}
          title={drawerProject.name}
          description={
            <>
              <span className="font-mono text-[12px]">{drawerProject.id}</span> · {limitsWords(drawerProject)}
            </>
          }
          actions={
            <Button size="xs" icon={<Pencil size={13} />} onClick={() => setEditing(drawerProject)}>
              Edit
            </Button>
          }
        >
          <div className="px-5 pt-2">
            <Tabs
              value={drawer!.tab}
              onChange={(t) => setDrawer({ id: drawerProject.id, tab: t })}
              items={[
                { id: "keys", label: "Access keys" },
                { id: "storage", label: "Storage" },
                { id: "danger", label: "Danger zone" },
              ]}
            />
          </div>
          {drawer!.tab === "keys" ? (
            <ProjectKeysPanel project={drawerProject} onCreated={(key: APIKey, secret: string) => (setRevealed({ projectId: drawerProject.id, key: secret, name: key.name, scopes: key.scopes }), setDrawer(null))} />
          ) : drawer!.tab === "storage" ? (
            <StoragePanel project={drawerProject} />
          ) : (
            <DangerZone project={drawerProject} onDeleted={() => (setDrawer(null), projects.refetch())} />
          )}
        </Drawer>
      )}
      {editing && <ProjectDialog project={editing} onClose={() => setEditing(null)} onCreated={(id, key) => setRevealed({ projectId: id, key, name: "Default key", scopes: ["ingest"] })} />}
    </Page>
  );
}

/** A key is shown once: say so, make it easy to copy, and point at the next step. */
function NewKeyCard({ revealed, serverUrl, onDismiss }: { revealed: Revealed; serverUrl: string; onDismiss: () => void }) {
  const sends = !revealed.scopes || revealed.scopes.includes("ingest");
  return (
    <Card className="border-good/40 bg-good/[0.05] p-4">
      <div className="flex items-start gap-3">
        <KeyRound size={18} className="mt-0.5 flex-none text-good-text" />
        <div className="min-w-0 flex-1">
          <div className="text-[14px] font-semibold text-ink">
            Your new access key{revealed.name ? `: ${revealed.name}` : ""}
          </div>
          <p className="mt-0.5 text-[13px] text-ink-2">
            Copy it now. It is shown only this once; Pyloggle keeps only a fingerprint of it.
            {revealed.scopes && ` It can: ${revealed.scopes.map(scopeLabel).join(", ")}.`}
          </p>
          <div className="mt-3 flex items-center gap-2">
            <code className="min-w-0 select-all break-all rounded-md border border-border bg-surface px-3 py-2 font-mono text-[12px] text-ink">{revealed.key}</code>
            <CopyButton text={revealed.key} label="Copy key" size="sm" showLabel />
          </div>
          <p className="mt-2 text-xs text-ink-3">
            {sends ? "Next: put it in your app's settings, as shown under Connect your app." : <>To use it from a script: <code className="font-mono">{`curl -H 'X-API-Key: …' '${serverUrl}/v1/q/issues?status=open'`}</code></>}
          </p>
        </div>
        <Button size="xs" variant="ghost" onClick={onDismiss}>
          I've saved it
        </Button>
      </div>
    </Card>
  );
}

function ProjectDialog({ project, onClose, onCreated }: { project: Partial<Project>; onClose: () => void; onCreated: (id: string, key: string) => void }) {
  const { create, update } = useProjectMutations();
  const isNew = !project.id;
  const [p, setP] = useState<Partial<Project>>({ max_events_per_second: 0, max_storage_gb: 0, retention_days: 0, ...project });
  const [idTouched, setIdTouched] = useState(false);
  const upd = (patch: Partial<Project>) => setP((x) => ({ ...x, ...patch }));
  const m = isNew ? create : update;
  return (
    <Dialog open onClose={onClose} title={isNew ? "New project" : `Edit ${project.name}`}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          const body = { ...p, max_events_per_second: Number(p.max_events_per_second) || 0, max_storage_gb: Number(p.max_storage_gb) || 0, retention_days: Number(p.retention_days) || 0 };
          if (isNew) create.mutate(body, { onSuccess: (r) => (onCreated(r.project.id, r.api_key), onClose()) });
          else update.mutate({ ...body, id: p.id! }, { onSuccess: onClose });
        }}
      >
        <Field label="Name">
          <Input required autoFocus value={p.name ?? ""} onChange={(e) => upd({ name: e.target.value, ...(isNew && !idTouched ? { id: slug(e.target.value) } : {}) })} placeholder="Checkout" />
        </Field>
        <Field label="Project id" hint="Used in your apps' settings. Lowercase letters, digits and dashes; it can't be changed later.">
          <Input required disabled={!isNew} value={p.id ?? ""} onChange={(e) => (setIdTouched(true), upd({ id: slug(e.target.value) }))} placeholder="checkout" className="font-mono text-[12px]" />
        </Field>
        <Field label="Description" hint="Optional.">
          <Input value={p.description ?? ""} onChange={(e) => upd({ description: e.target.value })} placeholder="Payments and checkout services" />
        </Field>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label="Messages per second" hint="0 for no limit">
            <Input type="number" min={0} value={p.max_events_per_second ?? 0} onChange={(e) => upd({ max_events_per_second: Number(e.target.value) })} />
          </Field>
          <Field label="Storage limit (GB)" hint="0 for no limit">
            <Input type="number" min={0} value={p.max_storage_gb ?? 0} onChange={(e) => upd({ max_storage_gb: Number(e.target.value) })} />
          </Field>
          <Field label="Keep data for (days)" hint="0 for the server's default">
            <Input type="number" min={0} value={p.retention_days ?? 0} onChange={(e) => upd({ retention_days: Number(e.target.value) })} />
          </Field>
        </div>
        {m.isError && (
          <div role="alert" className="text-[13px] text-critical-text">
            {(m.error as Error).message}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" loading={m.isPending}>
            {isNew ? "Create project" : "Save changes"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function slug(s: string): string {
  return s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9-_]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
}
