import type { AlertFiring } from "@/api/types";

/** What each kind of alert watches for, in plain words. */
export const ALERT_KIND: Record<string, { label: string; sentence: string }> = {
  error_rate: { label: "Too many errors", sentence: "errors happen faster than a limit" },
  issue_spike: { label: "A problem suddenly jumped", sentence: "a problem suddenly happens much more often" },
  new_issue: { label: "A new problem appeared", sentence: "a new problem appears" },
  regression: { label: "A fixed problem came back", sentence: "a problem marked as fixed comes back" },
  log_query: { label: "Messages matched a search", sentence: "messages matching a search cross a limit" },
};

export function alertKindLabel(kind: string): string {
  return ALERT_KIND[kind]?.label ?? kind.replace(/_/g, " ");
}

/** The rule name stored with a firing (the payload carries the rule as it was when it fired). */
export function firingRuleName(f: AlertFiring): string {
  const p = f.payload as { rule?: { name?: string } } | null;
  return p?.rule?.name || "Alert";
}
