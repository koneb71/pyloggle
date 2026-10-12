import type { AlertFiring, AlertRule } from "@/api/types";

/** What each kind of alert watches for, in plain words. */
export const ALERT_KIND: Record<string, { label: string; sentence: string }> = {
  error_rate: { label: "Too many errors", sentence: "errors happen faster than a limit" },
  issue_spike: { label: "A problem suddenly jumped", sentence: "a problem suddenly happens much more often" },
  new_issue: { label: "A new problem appeared", sentence: "a new problem appears" },
  regression: { label: "A fixed problem came back", sentence: "a problem marked as fixed comes back" },
  log_query: { label: "Messages matched a search", sentence: "messages matching a search cross a limit" },
  daily_recap: { label: "Recap of yesterday", sentence: "a recap of yesterday, every day" },
};

export function alertKindLabel(kind: string): string {
  return ALERT_KIND[kind]?.label ?? kind.replace(/_/g, " ");
}

/** The rule name stored with a firing (the payload carries the rule as it was when it fired). */
export function firingRuleName(f: AlertFiring): string {
  const p = f.payload as { rule?: { name?: string } } | null;
  return p?.rule?.name || "Alert";
}

export const COMPARISON_WORDS: { value: string; label: string; symbol: string }[] = [
  { value: "gt", label: "more than", symbol: ">" },
  { value: "gte", label: "at least", symbol: "≥" },
  { value: "lt", label: "fewer than", symbol: "<" },
  { value: "lte", label: "at most", symbol: "≤" },
  { value: "eq", label: "exactly", symbol: "=" },
];

export function comparisonWords(op?: string): string {
  return COMPARISON_WORDS.find((c) => c.value === (op || "gt"))?.label ?? "more than";
}

/** "5 minutes", "1 hour", "90 seconds": a window or wait in words. */
export function secondsWords(sec: number): string {
  const unit = (n: number, w: string) => `${n} ${w}${n === 1 ? "" : "s"}`;
  if (sec <= 0) return "no time";
  if (sec % 3600 === 0) return unit(sec / 3600, "hour");
  if (sec % 60 === 0) return unit(sec / 60, "minute");
  return unit(sec, "second");
}

function trimNumber(n: number): string {
  return Number.isInteger(n) ? n.toLocaleString() : (+n.toFixed(2)).toLocaleString();
}

/** What a rule watches, as one sentence starting after "Alert when". */
export function ruleCondition(r: Pick<AlertRule, "condition_type" | "threshold" | "query" | "comparison" | "time_window_seconds">): string {
  const win = secondsWords(r.time_window_seconds);
  switch (r.condition_type) {
    case "error_rate":
      return `errors happen more than ${trimNumber(r.threshold * 60)} times a minute`;
    case "issue_spike":
      return `one problem happens ${trimNumber(r.threshold)} more times than in the ${win} before`;
    case "new_issue":
      return "a new problem appears";
    case "regression":
      return "a problem marked as fixed comes back";
    case "log_query":
      return `${comparisonWords(r.comparison)} ${trimNumber(r.threshold)} ${r.threshold === 1 ? "message matches" : "messages match"} "${r.query ?? ""}"`;
  }
  return r.condition_type;
}

/** "08:00": an hour of the day. */
export function hourWords(h: number): string {
  return `${String(Math.min(23, Math.max(0, Math.round(h)))).padStart(2, "0")}:00`;
}

/** The rule card's main sentence. */
export function ruleSentence(r: Pick<AlertRule, "condition_type" | "threshold" | "query" | "comparison" | "time_window_seconds" | "service" | "env" | "http_route" | "http_method" | "send_hour" | "timezone">): string {
  if (r.condition_type === "daily_recap") {
    return `Sends a recap of yesterday every day at ${hourWords(r.send_hour ?? 8)} (${r.timezone || "UTC"}), ${ruleScope({ ...r, http_route: "", http_method: "" }).replace(/^in /, "for ")}.`;
  }
  return `Alert when ${ruleCondition(r)} ${ruleScope(r)}.`;
}

/** The time zone this browser is in, for new recap rules. */
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/** Every IANA time zone the browser knows, with UTC first. */
export function timeZones(): string[] {
  let all: string[] = [];
  try {
    all = Intl.supportedValuesOf("timeZone");
  } catch {
    all = [];
  }
  return ["UTC", ...all.filter((z) => z !== "UTC")];
}

/** How often a rule can alert about the same thing. */
export function ruleRepeat(kind: string): string {
  switch (kind) {
    case "new_issue":
      return "each problem alerts once";
    case "regression":
      return "each comeback alerts once";
    case "issue_spike":
      return "each problem alerts at most once a day";
    case "error_rate":
    case "log_query":
      return "alerts when it starts, again only after it recovers";
  }
  return "";
}

/** Where a rule looks: "in api / production, POST /orders" or "in all apps". */
export function ruleScope(r: Pick<AlertRule, "service" | "env" | "http_route" | "http_method">): string {
  const where = [r.service || "all apps", r.env].filter(Boolean).join(" / ");
  const route = r.http_route ? `, ${[r.http_method, r.http_route].filter(Boolean).join(" ")}` : "";
  return `in ${where}${route}`;
}

/** Who hears about it: "e-mails 2 people · Slack · Telegram · 1 webhook". */
export function ruleRecipients(r: Pick<AlertRule, "notification_emails" | "webhook_urls" | "telegram_chat_ids" | "slack_webhook_urls">): string {
  const parts: string[] = [];
  const n = (r.notification_emails ?? []).length;
  if (n) parts.push(`e-mails ${n === 1 ? "1 person" : `${n} people`}`);
  if ((r.slack_webhook_urls ?? []).length) parts.push("Slack");
  if ((r.telegram_chat_ids ?? []).length) parts.push("Telegram");
  const w = (r.webhook_urls ?? []).length;
  if (w) parts.push(w === 1 ? "1 webhook" : `${w} webhooks`);
  return parts.length ? parts.join(" · ") : "nobody yet";
}

/** The form edits minutes and per-minute rates; rules store seconds and per-second rates. */
export interface RuleForm {
  windowMinutes: number;
  cooldownMinutes: number;
  /** error_rate: errors per minute; other kinds: the threshold as stored. */
  threshold: number;
}

export function formFromRule(r: Pick<AlertRule, "condition_type" | "threshold" | "time_window_seconds" | "cooldown_seconds">): RuleForm {
  return {
    windowMinutes: Math.max(1, Math.round((r.time_window_seconds || 300) / 60)),
    cooldownMinutes: Math.round((r.cooldown_seconds ?? 900) / 60),
    threshold: r.condition_type === "error_rate" ? +(r.threshold * 60).toFixed(4) : r.threshold,
  };
}

export function ruleFromForm(conditionType: string, f: RuleForm): Pick<AlertRule, "threshold" | "time_window_seconds" | "cooldown_seconds"> {
  return {
    time_window_seconds: Math.max(1, Math.round(f.windowMinutes)) * 60,
    cooldown_seconds: Math.max(0, Math.round(f.cooldownMinutes)) * 60,
    threshold: conditionType === "error_rate" ? f.threshold / 60 : f.threshold,
  };
}

/** The sentence the notification itself carried (alerts sent since the server added it). */
export function firingSentence(f: AlertFiring): string | undefined {
  const s = (f.payload as { context?: { summary?: unknown } } | undefined)?.context?.summary;
  return typeof s === "string" && s.trim() ? s.trim() : undefined;
}

/** One sentence about what an alert reported, from its stored payload. */
export function firingSummary(f: AlertFiring): string {
  const p = (f.payload ?? {}) as { count?: number; error_count?: number; spikes?: unknown[]; issues?: unknown[] };
  switch (f.condition_type) {
    case "log_query":
      return `${(f.count ?? p.count ?? 0).toLocaleString()} matching messages`;
    case "error_rate":
      return `${(p.error_count ?? f.count ?? 0).toLocaleString()} errors`;
    case "issue_spike":
      return p.spikes?.length ? `${p.spikes.length === 1 ? "1 problem" : `${p.spikes.length} problems`} jumped` : "A problem jumped";
    case "new_issue":
      return p.issues?.length ? `${p.issues.length === 1 ? "1 new problem" : `${p.issues.length} new problems`}` : "New problem";
    case "regression":
      return p.issues?.length ? `${p.issues.length === 1 ? "1 problem" : `${p.issues.length} problems`} came back` : "A problem came back";
    case "daily_recap":
      return "Recap of yesterday";
  }
  return "";
}
