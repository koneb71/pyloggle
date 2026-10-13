import type { AuditEntry, Project } from "@/api/types";

/** Key permissions in words; the technical scope stays in the tooltip. */
export const SCOPE_WORDS: Record<string, { label: string; explain: string }> = {
  ingest: { label: "Send data", explain: "Send logs, errors, traces and metrics (what an app or exporter needs)" },
  read: { label: "View", explain: "Read logs, issues, traces and metrics (for scripts and dashboards)" },
  write: { label: "Make changes", explain: "Change issue states, comments, saved searches and alert rules" },
};

export function scopeLabel(s: string): string {
  return SCOPE_WORDS[s]?.label ?? s;
}

/** A project's limits in one line: "Up to 500 messages a second · 10 GB · keeps 14 days". */
export function limitsWords(p: Pick<Project, "max_events_per_second" | "max_storage_gb" | "retention_days">): string {
  const parts = [p.max_events_per_second ? `Up to ${p.max_events_per_second.toLocaleString()} messages a second` : "", p.max_storage_gb ? `${p.max_storage_gb} GB of storage` : "", p.retention_days ? `keeps ${p.retention_days} days` : ""].filter(Boolean);
  return parts.length ? parts.join(" · ") : "No limits";
}

const AUDIT_WORDS: Record<string, string> = {
  "project.create": "created the project",
  "project.update": "changed the project's settings",
  "project.delete": "deleted the project",
  "key.create": "created an access key",
  "key.revoke": "disabled an access key",
  "key.rotate": "replaced the access key",
  "user.purge": "erased one person's data",
  "logs.clear": "cleared all messages",
};

/** "created an access key": an admin action in words. */
export function auditWords(a: Pick<AuditEntry, "action">): string {
  return AUDIT_WORDS[a.action] ?? a.action.replace(/[._]/g, " ");
}
