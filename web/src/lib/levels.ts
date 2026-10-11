export const LEVELS = ["debug", "info", "warn", "error", "fatal"] as const;

// Series colors come from the validated palette in styles.css (CSS custom properties) so charts,
// badges and legends agree in both themes.
export function levelVar(level: string): string {
  const l = LEVELS.includes(level as (typeof LEVELS)[number]) ? level : "other";
  return `var(--lvl-${l})`;
}

export const STATUS_LABEL: Record<string, string> = { open: "Open", resolved: "Resolved", ignored: "Ignored", regressed: "Regressed" };

export function levelRank(level: string): number {
  const i = LEVELS.indexOf(level as (typeof LEVELS)[number]);
  return i < 0 ? 1 : i;
}
