export function shortId(id: string, n = 8): string {
  return id ? id.slice(0, n) : "";
}

export function basename(p: string): string {
  const i = Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\"));
  return i >= 0 ? p.slice(i + 1) : p;
}

export function lastSegments(p: string, n = 2): string {
  const parts = p.replace(/\\/g, "/").split("/");
  return parts.slice(-n).join("/");
}

export function pluralize(n: number, one: string, many = one + "s"): string {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

export interface Delta {
  /** "+12%", "−8%", "2.4×", "New" or "No change". */
  text: string;
  direction: "up" | "down" | "flat";
}

/**
 * Change from the previous period, worded for people: small changes read as "No change",
 * doublings and more as a multiple ("3.1×"), everything else as a whole percentage.
 */
export function fmtDelta(current: number, previous: number): Delta | null {
  if (!isFinite(current) || !isFinite(previous)) return null;
  if (previous === 0) return current === 0 ? { text: "No change", direction: "flat" } : { text: "New", direction: "up" };
  const pct = ((current - previous) / Math.abs(previous)) * 100;
  if (Math.abs(pct) < 0.5) return { text: "No change", direction: "flat" };
  const direction = pct > 0 ? "up" : "down";
  const ratio = current / previous;
  if (ratio >= 2) return { text: `${ratio >= 10 ? Math.round(ratio) : ratio.toFixed(1)}×`, direction };
  return { text: `${pct > 0 ? "+" : "−"}${Math.round(Math.abs(pct))}%`, direction };
}
