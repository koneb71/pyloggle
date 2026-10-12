export const LEVELS = ["debug", "info", "warn", "error", "fatal"] as const;

// Series colors come from the validated palette in styles.css (CSS custom properties) so charts,
// badges and legends agree in both themes.
export function levelVar(level: string): string {
  const l = LEVELS.includes(level as (typeof LEVELS)[number]) ? level : "other";
  return `var(--lvl-${l})`;
}

const LEVEL_LABEL: Record<string, string> = { debug: "Debug", info: "Info", warn: "Warning", warning: "Warning", error: "Error", fatal: "Fatal", critical: "Fatal" };

/** "warn" → "Warning": severity names in sentence case. */
export function levelLabel(level: string): string {
  const l = (level || "").toLowerCase();
  return LEVEL_LABEL[l] ?? (l ? l.charAt(0).toUpperCase() + l.slice(1) : "Unknown");
}

export const STATUS_LABEL: Record<string, string> = { open: "Unresolved", resolved: "Resolved", ignored: "Ignored", regressed: "Came back" };

export function levelRank(level: string): number {
  const i = LEVELS.indexOf(level as (typeof LEVELS)[number]);
  return i < 0 ? 1 : i;
}
