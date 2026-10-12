import { useState } from "react";
import { Bell, BellOff, ExternalLink, FlaskConical, Pencil, Plus } from "lucide-react";
import { previewAlertRule, previewRecap, useAlertFirings, useAlertRules, useLogFacets, useSaveAlertRule } from "@/api/hooks";
import type { AlertFiring, AlertRule, Me } from "@/api/types";
import { Badge, Button, Card, Checkbox, Dialog, EmptyState, Field, Input, JsonView, MetaLine, Page, QueryState, Section, Select, Skeleton, Textarea, TimeAgo, friendlyError, toast } from "@/components/ui";
import { fmtTs, fmtWhen } from "@/lib/time";
import { ALERT_KIND, COMPARISON_WORDS, alertKindLabel, browserTimeZone, firingSentence, firingSummary, formFromRule, hourWords, ruleFromForm, ruleRecipients, ruleRepeat, ruleSentence, secondsWords, timeZones, type RuleForm } from "./alert-words";

export function Compare(op: string | undefined, value: number, threshold: number): boolean {
  switch (op || "gt") {
    case "gt":
      return value > threshold;
    case "gte":
      return value >= threshold;
    case "lt":
      return value < threshold;
    case "lte":
      return value <= threshold;
    case "eq":
      return value === threshold;
  }
  return false;
}

const blank = (): Partial<AlertRule> => ({ name: "", description: "", condition_type: "regression", threshold: 0.05, time_window_seconds: 300, cooldown_seconds: 900, enabled: true, notification_channels: [], notification_emails: [], webhook_urls: [], telegram_chat_ids: [], slack_webhook_urls: [], query: "", comparison: "gt" });

export function AlertsPage({ me }: { me: Me }) {
  const rules = useAlertRules();
  const firings = useAlertFirings();
  const save = useSaveAlertRule();
  const [editing, setEditing] = useState<Partial<AlertRule> | null>(null);
  const canEdit = me.is_admin;
  const lastSent = (id: string) => firings.data?.items.find((f) => f.rule_id === id)?.fired_at;
  return (
    <Page
      title="Alerts"
      description="Get told when something goes wrong, by e-mail, Slack, Telegram or a webhook. Rules are checked every minute."
      actions={
        canEdit && (
          <Button variant="primary" icon={<Plus size={14} />} onClick={() => setEditing(blank())}>
            New alert
          </Button>
        )
      }
    >
      {!me.features.alerting && (
        <Card className="border-warning/40 bg-warning/[0.07] px-4 py-3 text-[13px]">
          <div className="font-medium text-ink">Alerts are switched off on this server</div>
          <div className="mt-0.5 text-ink-2">
            You can set rules up, but nothing is checked or sent until an administrator turns alerting on <span className="text-ink-3">(setting ALERTING_ENABLED)</span>.
          </div>
        </Card>
      )}
      {!canEdit && <p className="text-[13px] text-ink-3">Only administrators can create or change alerts.</p>}

      <Section title="Rules" description="What Pyloggle watches, and who it tells." flush>
        <QueryState
          query={rules}
          isEmpty={(d) => d.items.length === 0}
          skeleton={<Skeleton rows={4} className="p-4" />}
          empty={
            <EmptyState
              icon={<BellOff size={18} />}
              title="No alerts yet"
              hint="Create one to be told when errors jump, a new problem appears, a fixed problem comes back, or a search finds (or stops finding) messages."
              action={canEdit && <Button onClick={() => setEditing(blank())}>Create the first alert</Button>}
            />
          }
        >
          {(d) => (
            <ul className="divide-y divide-border">
              {d.items.map((r) => {
                const sent = lastSent(r.id);
                return (
                  <li key={r.id} className="flex items-start gap-3 px-4 py-3.5">
                    <span className={r.enabled ? "mt-0.5 text-accent" : "mt-0.5 text-ink-3"}>{r.enabled ? <Bell size={16} /> : <BellOff size={16} />}</span>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-[14px] font-semibold text-ink">{r.name}</span>
                        {r.enabled ? <Badge tone="good">On</Badge> : <Badge>Off</Badge>}
                      </div>
                      <div className="mt-0.5 text-[13px] text-ink-2">
                        {ruleSentence(r)}
                      </div>
                      <MetaLine className="mt-1">
                        {r.condition_type !== "daily_recap" && `checks the last ${secondsWords(r.time_window_seconds)}`}
                        {ruleRepeat(r.condition_type) || null}
                        {ruleRecipients(r)}
                        <span>{sent ? <>last sent <TimeAgo iso={sent} /></> : "never sent"}</span>
                      </MetaLine>
                    </div>
                    {canEdit && (
                      <div className="flex flex-none items-center gap-1">
                        <Button
                          size="xs"
                          variant="ghost"
                          onClick={() => save.mutate({ ...r, enabled: !r.enabled }, { onError: (e) => toast.error(`Couldn't turn the alert ${r.enabled ? "off" : "on"}`, friendlyError(e).detail) })}
                          loading={save.isPending && save.variables?.id === r.id}
                        >
                          {r.enabled ? "Turn off" : "Turn on"}
                        </Button>
                        <Button size="xs" icon={<Pencil size={13} />} onClick={() => setEditing(r)}>
                          Edit
                        </Button>
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </QueryState>
      </Section>

      <Section title="Alerts sent" description="The most recent notifications, newest first." flush>
        <QueryState
          query={firings}
          isEmpty={(d) => d.items.length === 0}
          skeleton={<Skeleton rows={3} className="p-4" />}
          empty={<EmptyState title="Nothing sent yet" hint="When a rule fires, the notification shows up here." className="py-8" />}
        >
          {(d) => (
            <ul className="divide-y divide-border">
              {d.items.map((f) => (
                <Firing key={f.id} f={f} ruleName={rules.data?.items.find((r) => r.id === f.rule_id)?.name} />
              ))}
            </ul>
          )}
        </QueryState>
      </Section>
      {editing && <RuleDialog rule={editing} onClose={() => setEditing(null)} conditions={rules.data?.conditions ?? Object.keys(ALERT_KIND)} />}
    </Page>
  );
}

function Firing({ f, ruleName }: { f: AlertFiring; ruleName?: string }) {
  const [open, setOpen] = useState(false);
  const sentence = firingSentence(f);
  return (
    <li className="px-4 py-3 text-[13px]">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <Bell size={14} className="flex-none text-ink-3" />
        <span className="font-medium text-ink">{ruleName ?? "Deleted rule"}</span>
        <span className="text-ink-2">{alertKindLabel(f.condition_type)}</span>
        {!sentence && <span className="text-ink-3">{firingSummary(f)}</span>}
        <span className="ml-auto flex items-center gap-3 text-xs text-ink-3">
          <span title={`Looked at ${fmtTs(f.window_start)} to ${fmtTs(f.window_end)}`}>{fmtWhen(f.fired_at)}</span>
          {f.url && (
            <a href={f.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-medium text-accent hover:underline">
              Open <ExternalLink size={12} />
            </a>
          )}
          <button type="button" className="hover:text-ink" aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? "Hide technical details" : "Technical details"}
          </button>
        </span>
      </div>
      {sentence && <p className="ml-[26px] mt-1 text-ink-2">{sentence}</p>}
      {(f.service || f.env) && <MetaLine className="ml-[26px] mt-0.5">{[f.service, f.env].filter(Boolean).join(" / ")}</MetaLine>}
      {open && (
        <div className="mt-2 rounded-md bg-surface-2 p-3">
          <JsonView value={f.payload} />
        </div>
      )}
    </li>
  );
}

const KIND_OPTIONS: { value: string; label: string }[] = [
  { value: "regression", label: "a problem marked as fixed comes back" },
  { value: "new_issue", label: "a new problem appears" },
  { value: "issue_spike", label: "a problem suddenly happens much more" },
  { value: "error_rate", label: "errors happen too often" },
  { value: "log_query", label: "messages matching a search cross a limit" },
  { value: "daily_recap", label: "a recap of yesterday, every day" },
];

function RuleDialog({ rule, onClose, conditions }: { rule: Partial<AlertRule>; onClose: () => void; conditions: string[] }) {
  const save = useSaveAlertRule();
  const facets = useLogFacets({ from: "-7d", fields: ["service", "env"], attr_keys: 0 });
  const [r, setR] = useState<Partial<AlertRule>>({ comparison: "gt", query: "", ...rule });
  const [form, setForm] = useState<RuleForm>(() => formFromRule({ condition_type: rule.condition_type ?? "error_rate", threshold: rule.threshold ?? 0.05, time_window_seconds: rule.time_window_seconds ?? 300, cooldown_seconds: rule.cooldown_seconds ?? 900 }));
  const [emails, setEmails] = useState((rule.notification_emails ?? []).join(", "));
  const [webhooks, setWebhooks] = useState((rule.webhook_urls ?? []).join("\n"));
  const [telegram, setTelegram] = useState((rule.telegram_chat_ids ?? []).join(", "));
  const [slack, setSlack] = useState((rule.slack_webhook_urls ?? []).join("\n"));
  const [preview, setPreview] = useState<{ count: number } | { error: string } | null>(null);
  const [recapText, setRecapText] = useState<{ text: string } | { error: string } | null>(null);
  const [testing, setTesting] = useState(false);
  const upd = (patch: Partial<AlertRule>) => setR((x) => ({ ...x, ...patch }));
  const updForm = (patch: Partial<RuleForm>) => setForm((x) => ({ ...x, ...patch }));
  const split = (s: string) =>
    s
      .split(/[\n,]/)
      .map((x) => x.trim())
      .filter(Boolean);
  const kind = r.condition_type ?? "error_rate";
  const isQuery = kind === "log_query";
  const isRecap = kind === "daily_recap";
  const needsNumber = kind === "error_rate" || kind === "issue_spike" || isQuery;
  const changeKind = (k: string) => {
    upd({ condition_type: k });
    // Sensible starting limits per kind, in the units the form shows.
    if (k === "error_rate") updForm({ threshold: 3 });
    if (k === "issue_spike") updForm({ threshold: 20 });
    if (k === "log_query") updForm({ threshold: 0 });
    if (k === "daily_recap") upd({ condition_type: k, send_hour: r.send_hour ?? 8, timezone: r.timezone || browserTimeZone() });
    setRecapText(null);
  };
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const ne = split(emails);
    const nw = split(webhooks);
    const nt = split(telegram);
    const ns = split(slack);
    const channels = [ne.length && "email", nw.length && "webhook", nt.length && "telegram", ns.length && "slack"].filter(Boolean) as string[];
    save.mutate({ ...r, ...ruleFromForm(kind, { ...form, threshold: Number(form.threshold) || 0 }), notification_emails: ne, webhook_urls: nw, telegram_chat_ids: nt, slack_webhook_urls: ns, notification_channels: channels }, { onSuccess: onClose });
  };
  const test = async () => {
    setTesting(true);
    try {
      const res = await previewAlertRule({ query: r.query ?? "", window_seconds: Math.max(1, form.windowMinutes) * 60, service: r.service, env: r.env, http_route: r.http_route, http_method: r.http_method });
      setPreview({ count: res.count });
    } catch (err) {
      setPreview({ error: err instanceof Error ? err.message : String(err) });
    } finally {
      setTesting(false);
    }
  };
  const testRecap = async () => {
    setTesting(true);
    try {
      setRecapText(await previewRecap({ name: r.name, service: r.service, env: r.env, timezone: r.timezone }));
    } catch (err) {
      setRecapText({ error: err instanceof Error ? err.message : String(err) });
    } finally {
      setTesting(false);
    }
  };
  const zones = isRecap ? timeZones() : [];
  const num = "w-24";
  return (
    <Dialog open onClose={onClose} title={rule.id ? "Edit alert" : "New alert"} maxWidth={760}>
      <form onSubmit={submit} className="space-y-5">
        <Field label="Name" hint="Shown in every notification.">
          <Input required autoFocus={!rule.id} value={r.name ?? ""} onChange={(e) => upd({ name: e.target.value })} placeholder="e.g. Checkout errors" />
        </Field>

        <fieldset className="min-w-0 space-y-3 rounded-lg border border-border p-4 text-[13px] text-ink-2">
          <legend className="px-1 text-[13px] font-semibold text-ink">When</legend>
          <div className="flex flex-wrap items-center gap-2">
            <span>{isRecap ? "Send me" : "Alert me when"}</span>
            <Select value={kind} onChange={(e) => changeKind(e.target.value)} className="w-auto min-w-72" aria-label="Condition">
              {KIND_OPTIONS.filter((k) => conditions.includes(k.value)).map((k) => (
                <option key={k.value} value={k.value}>
                  {k.label}
                </option>
              ))}
            </Select>
          </div>
          {kind === "error_rate" && (
            <div className="flex flex-wrap items-center gap-2">
              <span>More than</span>
              <Input type="number" min={0} step="any" className={num} value={form.threshold} onChange={(e) => updForm({ threshold: Number(e.target.value) })} aria-label="Errors per minute" />
              <span>errors a minute, on average over the last</span>
              <Input type="number" min={1} className={num} value={form.windowMinutes} onChange={(e) => updForm({ windowMinutes: Number(e.target.value) })} aria-label="Minutes" />
              <span>minutes</span>
            </div>
          )}
          {kind === "issue_spike" && (
            <div className="flex flex-wrap items-center gap-2">
              <span>By more than</span>
              <Input type="number" min={1} className={num} value={form.threshold} onChange={(e) => updForm({ threshold: Number(e.target.value) })} aria-label="More times" />
              <span>times, comparing the last</span>
              <Input type="number" min={1} className={num} value={form.windowMinutes} onChange={(e) => updForm({ windowMinutes: Number(e.target.value) })} aria-label="Minutes" />
              <span>minutes with the same time before</span>
            </div>
          )}
          {(kind === "new_issue" || kind === "regression") && (
            <div className="flex flex-wrap items-center gap-2">
              <span>Check every minute, looking at the last</span>
              <Input type="number" min={1} className={num} value={form.windowMinutes} onChange={(e) => updForm({ windowMinutes: Number(e.target.value) })} aria-label="Minutes" />
              <span>minutes</span>
            </div>
          )}
          {isQuery && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span>Messages matching</span>
                <Input value={r.query ?? ""} onChange={(e) => (upd({ query: e.target.value }), setPreview(null))} className="min-w-64 flex-1 font-mono text-[12px]" placeholder="level:error service:api" spellCheck={false} aria-label="Search" />
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <span>number</span>
                <Select value={r.comparison ?? "gt"} onChange={(e) => upd({ comparison: e.target.value })} className="w-auto" aria-label="Comparison">
                  {COMPARISON_WORDS.map((c) => (
                    <option key={c.value} value={c.value}>
                      {c.label}
                    </option>
                  ))}
                </Select>
                <Input type="number" min={0} step="1" className={num} value={form.threshold} onChange={(e) => updForm({ threshold: Number(e.target.value) })} aria-label="Limit" />
                <span>in the last</span>
                <Input type="number" min={1} className={num} value={form.windowMinutes} onChange={(e) => updForm({ windowMinutes: Number(e.target.value) })} aria-label="Minutes" />
                <span>minutes</span>
              </div>
              <p className="text-xs text-ink-3">Same search as on the Logs page. Use "fewer than 1" to be told when something stops happening, like a nightly job.</p>
              <div className="flex flex-wrap items-center gap-3 rounded-md bg-surface-2 px-3 py-2">
                <Button size="xs" icon={<FlaskConical size={13} />} onClick={test} loading={testing} disabled={!r.query}>
                  Test this alert
                </Button>
                {preview && "error" in preview ? (
                  <span className="text-critical-text">{preview.error}</span>
                ) : preview ? (
                  <span className="text-ink-2">
                    <span className="font-semibold tabular text-ink">{preview.count.toLocaleString()}</span> {preview.count === 1 ? "message matches" : "messages match"} right now, so this alert{" "}
                    <span className="font-medium text-ink">{Compare(r.comparison, preview.count, Number(form.threshold) || 0) ? "would fire" : "would stay quiet"}</span>.
                  </span>
                ) : (
                  <span className="text-ink-3">Runs the search over the last {form.windowMinutes || 5} minutes without saving.</span>
                )}
              </div>
            </>
          )}
          {isRecap && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span>at</span>
                <Select value={String(r.send_hour ?? 8)} onChange={(e) => upd({ send_hour: Number(e.target.value) })} className="w-auto" aria-label="Send at">
                  {Array.from({ length: 24 }, (_, h) => (
                    <option key={h} value={h}>
                      {hourWords(h)}
                    </option>
                  ))}
                </Select>
                <span>in</span>
                <Select value={r.timezone || "UTC"} onChange={(e) => (upd({ timezone: e.target.value }), setRecapText(null))} className="w-auto max-w-72" aria-label="Time zone">
                  {(zones.includes(r.timezone || "UTC") ? zones : [r.timezone || "UTC", ...zones]).map((z) => (
                    <option key={z} value={z}>
                      {z.replace(/_/g, " ")}
                    </option>
                  ))}
                </Select>
              </div>
              <p className="text-xs text-ink-3">
                Yesterday's errors compared with the day before, new problems, problems that came back, the ones that happened most, apps that need a look, anything still going on, and the alerts sent. It goes out once a day, even on a quiet day.
              </p>
              <div className="space-y-2 rounded-md bg-surface-2 px-3 py-2">
                <div className="flex flex-wrap items-center gap-3">
                  <Button size="xs" icon={<FlaskConical size={13} />} onClick={testRecap} loading={testing}>
                    Test this alert
                  </Button>
                  {!recapText && <span className="text-ink-3">Builds yesterday's recap now, without sending it.</span>}
                  {recapText && "error" in recapText && <span className="text-critical-text">{recapText.error}</span>}
                </div>
                {recapText && "text" in recapText && <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded border border-border bg-surface p-3 text-xs leading-5 text-ink [overflow-wrap:anywhere]">{recapText.text}</pre>}
              </div>
            </>
          )}
          {!needsNumber && !isRecap && <p className="text-xs text-ink-3">{kind === "regression" ? "Fires when an issue you resolved happens again." : "Fires the first time Pyloggle sees a new kind of error."}</p>}
        </fieldset>

        <fieldset className="min-w-0 space-y-3 rounded-lg border border-border p-4 text-[13px] text-ink-2">
          <legend className="px-1 text-[13px] font-semibold text-ink">Where</legend>
          <div className="flex flex-wrap items-center gap-2">
            <span>For</span>
            <Input list="alert-apps" value={r.service ?? ""} onChange={(e) => upd({ service: e.target.value })} placeholder="all apps" className="w-44" aria-label="App" />
            <span>in</span>
            <Input list="alert-envs" value={r.env ?? ""} onChange={(e) => upd({ env: e.target.value })} placeholder="all environments" className="w-44" aria-label="Environment" />
            <datalist id="alert-apps">{(facets.data?.fields.service ?? []).map((f) => <option key={f.value} value={f.value} />)}</datalist>
            <datalist id="alert-envs">{(facets.data?.fields.env ?? []).map((f) => <option key={f.value} value={f.value} />)}</datalist>
          </div>
          {!isRecap && <div className="flex flex-wrap items-center gap-2">
            <span>Only requests to</span>
            <Input value={r.http_method ?? ""} onChange={(e) => upd({ http_method: e.target.value.toUpperCase() })} placeholder="any method" className="w-28 font-mono text-[12px]" aria-label="Method" />
            <Input value={r.http_route ?? ""} onChange={(e) => upd({ http_route: e.target.value })} placeholder="any route, e.g. /orders" className="w-56 font-mono text-[12px]" aria-label="Route" />
            <span className="text-xs text-ink-3">(optional)</span>
          </div>}
        </fieldset>

        <fieldset className="min-w-0 space-y-3 rounded-lg border border-border p-4 text-[13px]">
          <legend className="px-1 text-[13px] font-semibold text-ink">Tell</legend>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="E-mail" hint="Addresses, separated by commas.">
              <Input value={emails} onChange={(e) => setEmails(e.target.value)} placeholder="oncall@example.com" />
            </Field>
            <Field label="Telegram" hint="Chat ids, separated by commas.">
              <Input value={telegram} onChange={(e) => setTelegram(e.target.value)} placeholder="-1001234567890" />
            </Field>
            <Field label="Slack" hint="Incoming webhook addresses, one per line. Empty uses the server's default.">
              <Textarea rows={2} value={slack} onChange={(e) => setSlack(e.target.value)} placeholder="https://hooks.slack.com/services/…" />
            </Field>
            <Field label="Webhook" hint="Addresses that receive the alert as JSON, one per line.">
              <Textarea rows={2} value={webhooks} onChange={(e) => setWebhooks(e.target.value)} placeholder="https://example.com/hooks/pyloggle" />
            </Field>
          </div>
          {!isRecap && <div className="flex flex-wrap items-center gap-2 text-ink-2">
            <span>After alerting, wait</span>
            <Input type="number" min={0} className={num} value={form.cooldownMinutes} onChange={(e) => updForm({ cooldownMinutes: Number(e.target.value) })} aria-label="Wait minutes" />
            <span>minutes before alerting again</span>
          </div>}
        </fieldset>

        <Checkbox label="Turned on" checked={r.enabled ?? true} onChange={(e) => upd({ enabled: e.target.checked })} />
        {save.isError && (
          <div role="alert" className="text-[13px] text-critical-text">
            {(save.error as Error).message}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" loading={save.isPending}>
            {rule.id ? "Save changes" : "Create alert"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
