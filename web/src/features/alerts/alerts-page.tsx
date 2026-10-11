import { useState } from "react";
import { Bell, BellOff, Pencil, Plus } from "lucide-react";
import { useAlertFirings, useAlertRules, useSaveAlertRule } from "@/api/hooks";
import type { AlertRule, Me } from "@/api/types";
import { PageHeader } from "@/components/shell/app-shell";
import { Badge, Button, Card, CardHeader, Checkbox, Dialog, EmptyState, ErrorState, Field, Input, JsonView, Select, Skeleton, Textarea, TimeAgo } from "@/components/ui";
import { fmtTs } from "@/lib/time";

const CONDITIONS: Record<string, { label: string; help: string; threshold: string }> = {
  error_rate: { label: "Error rate", help: "errors / events in the window exceeds the threshold (0–1)", threshold: "ratio, e.g. 0.05" },
  issue_spike: { label: "Issue spike", help: "any single issue has more than N occurrences in the window", threshold: "occurrences" },
  new_issue: { label: "New issue", help: "an issue was seen for the first time in the window", threshold: "unused" },
  regression: { label: "Regression", help: "a resolved issue reoccurred in the window", threshold: "unused" },
};

const blank = (): Partial<AlertRule> => ({ name: "", description: "", condition_type: "error_rate", threshold: 0.05, time_window_seconds: 300, cooldown_seconds: 900, enabled: true, notification_channels: [], notification_emails: [], webhook_urls: [], telegram_chat_ids: [] });

export function AlertsPage({ me }: { me: Me }) {
  const rules = useAlertRules();
  const firings = useAlertFirings();
  const [editing, setEditing] = useState<Partial<AlertRule> | null>(null);
  const canEdit = me.is_admin;
  return (
    <>
      <PageHeader title="Alerts" sub={me.features.alerting ? "Rules are evaluated every minute" : "Alerting is disabled on this server (ALERTING_ENABLED)"}>
        {canEdit && (
          <Button variant="primary" icon={<Plus size={13} />} onClick={() => setEditing(blank())}>
            New rule
          </Button>
        )}
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[1200px] flex-col gap-3 p-4">
          <Card>
            <CardHeader title="Rules" sub={rules.data ? `${rules.data.items.length} configured` : undefined} />
            {rules.isError && <ErrorState error={rules.error} retry={() => rules.refetch()} />}
            {rules.isPending ? (
              <Skeleton rows={4} className="p-3" />
            ) : (rules.data?.items.length ?? 0) === 0 ? (
              <EmptyState icon={<BellOff size={22} />} title="No alert rules" hint="Create a rule to be notified by email, webhook or Telegram when error rates spike, a new issue appears or a resolved issue regresses." action={canEdit && <Button onClick={() => setEditing(blank())}>Create the first rule</Button>} />
            ) : (
              <ul className="divide-y divide-border">
                {rules.data!.items.map((r) => (
                  <li key={r.id} className="flex items-start gap-3 px-3 py-2.5">
                    <span className={r.enabled ? "text-good" : "text-ink-3"}>{r.enabled ? <Bell size={15} /> : <BellOff size={15} />}</span>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-medium text-ink">{r.name}</span>
                        <Badge tone="accent">{CONDITIONS[r.condition_type]?.label ?? r.condition_type}</Badge>
                        {!r.enabled && <Badge>disabled</Badge>}
                      </div>
                      <div className="mt-0.5 text-xs text-ink-3">
                        {r.condition_type === "error_rate" && `> ${(r.threshold * 100).toFixed(1)}% errors`}
                        {r.condition_type === "issue_spike" && `> ${r.threshold} occurrences of one issue`}
                        {(r.condition_type === "new_issue" || r.condition_type === "regression") && CONDITIONS[r.condition_type].help}
                        {` · window ${r.time_window_seconds}s · cooldown ${r.cooldown_seconds}s`}
                        {r.service && ` · service ${r.service}`}
                        {r.env && ` · env ${r.env}`}
                        {r.http_route && ` · ${r.http_method || ""} ${r.http_route}`}
                      </div>
                      <div className="mt-1 flex flex-wrap gap-1 text-[11px] text-ink-2">
                        {r.notification_emails?.map((e) => (
                          <Badge key={e} tone="outline">
                            ✉ {e}
                          </Badge>
                        ))}
                        {r.webhook_urls?.map((u) => (
                          <Badge key={u} tone="outline" className="max-w-xs truncate">
                            ⇢ {u}
                          </Badge>
                        ))}
                        {r.telegram_chat_ids?.map((c) => (
                          <Badge key={c} tone="outline">
                            ✈ {c}
                          </Badge>
                        ))}
                      </div>
                    </div>
                    {canEdit && <Button size="xs" variant="ghost" icon={<Pencil size={12} />} onClick={() => setEditing(r)} aria-label={`Edit ${r.name}`} />}
                  </li>
                ))}
              </ul>
            )}
          </Card>
          <Card>
            <CardHeader title="Recent firings" />
            {firings.isPending ? (
              <Skeleton rows={3} className="p-3" />
            ) : (firings.data?.items.length ?? 0) === 0 ? (
              <EmptyState title="Nothing fired yet" className="py-6" />
            ) : (
              <ul className="divide-y divide-border">
                {firings.data!.items.map((f) => (
                  <Firing key={f.id} f={f} ruleName={rules.data?.items.find((r) => r.id === f.rule_id)?.name} />
                ))}
              </ul>
            )}
          </Card>
        </div>
      </div>
      {editing && <RuleDialog rule={editing} onClose={() => setEditing(null)} conditions={rules.data?.conditions ?? Object.keys(CONDITIONS)} />}
    </>
  );
}

function Firing({ f, ruleName }: { f: { id: string; rule_id: string; condition_type: string; fired_at: string; window_start: string; window_end: string; service: string; env: string; payload: unknown }; ruleName?: string }) {
  const [open, setOpen] = useState(false);
  return (
    <li className="px-3 py-2 text-xs">
      <button type="button" className="flex w-full items-center gap-2 text-left" onClick={() => setOpen(!open)}>
        <span className="font-medium text-ink">{ruleName ?? f.rule_id.slice(0, 8)}</span>
        <Badge tone="accent">{CONDITIONS[f.condition_type]?.label ?? f.condition_type}</Badge>
        {f.service && <Badge>{f.service}</Badge>}
        {f.env && <Badge tone="outline">{f.env}</Badge>}
        <span className="ml-auto text-ink-3">
          <TimeAgo iso={f.fired_at} /> · window {fmtTs(f.window_start)} → {fmtTs(f.window_end)}
        </span>
      </button>
      {open && <div className="mt-2 rounded-md bg-surface-2 p-2"><JsonView value={f.payload} /></div>}
    </li>
  );
}

function RuleDialog({ rule, onClose, conditions }: { rule: Partial<AlertRule>; onClose: () => void; conditions: string[] }) {
  const save = useSaveAlertRule();
  const [r, setR] = useState<Partial<AlertRule>>({ ...rule });
  const [emails, setEmails] = useState((rule.notification_emails ?? []).join(", "));
  const [webhooks, setWebhooks] = useState((rule.webhook_urls ?? []).join("\n"));
  const [telegram, setTelegram] = useState((rule.telegram_chat_ids ?? []).join(", "));
  const upd = (patch: Partial<AlertRule>) => setR((x) => ({ ...x, ...patch }));
  const split = (s: string) => s.split(/[\n,]/).map((x) => x.trim()).filter(Boolean);
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const ne = split(emails);
    const nw = split(webhooks);
    const nt = split(telegram);
    const channels = [ne.length && "email", nw.length && "webhook", nt.length && "telegram"].filter(Boolean) as string[];
    save.mutate({ ...r, notification_emails: ne, webhook_urls: nw, telegram_chat_ids: nt, notification_channels: channels, threshold: Number(r.threshold) || 0, time_window_seconds: Number(r.time_window_seconds) || 300, cooldown_seconds: Number(r.cooldown_seconds) || 0 }, { onSuccess: onClose });
  };
  const cond = CONDITIONS[r.condition_type ?? "error_rate"];
  return (
    <Dialog open onClose={onClose} title={rule.id ? "Edit rule" : "New alert rule"}>
      <form onSubmit={submit} className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Name" className="col-span-2">
            <Input required value={r.name ?? ""} onChange={(e) => upd({ name: e.target.value })} />
          </Field>
          <Field label="Condition" hint={cond?.help}>
            <Select value={r.condition_type} onChange={(e) => upd({ condition_type: e.target.value })}>
              {conditions.map((c) => (
                <option key={c} value={c}>
                  {CONDITIONS[c]?.label ?? c}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Threshold" hint={cond?.threshold}>
            <Input type="number" step="any" value={r.threshold ?? 0} onChange={(e) => upd({ threshold: Number(e.target.value) })} disabled={r.condition_type === "new_issue" || r.condition_type === "regression"} />
          </Field>
          <Field label="Window (seconds)">
            <Input type="number" min={60} value={r.time_window_seconds ?? 300} onChange={(e) => upd({ time_window_seconds: Number(e.target.value) })} />
          </Field>
          <Field label="Cooldown (seconds)">
            <Input type="number" min={0} value={r.cooldown_seconds ?? 900} onChange={(e) => upd({ cooldown_seconds: Number(e.target.value) })} />
          </Field>
          <Field label="Service (optional)">
            <Input value={r.service ?? ""} onChange={(e) => upd({ service: e.target.value })} />
          </Field>
          <Field label="Env (optional)">
            <Input value={r.env ?? ""} onChange={(e) => upd({ env: e.target.value })} />
          </Field>
          <Field label="HTTP route (optional)">
            <Input value={r.http_route ?? ""} onChange={(e) => upd({ http_route: e.target.value })} placeholder="/api/orders" />
          </Field>
          <Field label="HTTP method (optional)">
            <Input value={r.http_method ?? ""} onChange={(e) => upd({ http_method: e.target.value })} placeholder="POST" />
          </Field>
          <Field label="Emails" hint="comma separated" className="col-span-2">
            <Input value={emails} onChange={(e) => setEmails(e.target.value)} />
          </Field>
          <Field label="Webhook URLs" hint="one per line" className="col-span-2">
            <Textarea rows={2} value={webhooks} onChange={(e) => setWebhooks(e.target.value)} />
          </Field>
          <Field label="Telegram chat ids" hint="comma separated" className="col-span-2">
            <Input value={telegram} onChange={(e) => setTelegram(e.target.value)} />
          </Field>
        </div>
        <Checkbox label="Enabled" checked={r.enabled ?? true} onChange={(e) => upd({ enabled: e.target.checked })} />
        {save.isError && <div className="text-xs text-critical">{(save.error as Error).message}</div>}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" loading={save.isPending}>
            Save rule
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
