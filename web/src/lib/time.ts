export interface Range {
  from: string; // relative ("-1h") or absolute ISO
  to: string; // "" = now, or absolute ISO
}

export const QUICK_RANGES: { label: string; from: string }[] = [
  { label: "15m", from: "-15m" },
  { label: "1h", from: "-1h" },
  { label: "4h", from: "-4h" },
  { label: "24h", from: "-24h" },
  { label: "2d", from: "-2d" },
  { label: "7d", from: "-7d" },
  { label: "30d", from: "-30d" },
];

export const DEFAULT_RANGE: Range = { from: "-1h", to: "" };

const RELATIVE = /^-(\d+(?:\.\d+)?)([smhdw])$/;
const UNIT_MS: Record<string, number> = { s: 1e3, m: 6e4, h: 36e5, d: 864e5, w: 6048e5 };

export function resolveRange(r: Range, now = Date.now()): { from: Date; to: Date } {
  const to = r.to ? new Date(r.to) : new Date(now);
  let from: Date;
  const m = RELATIVE.exec(r.from || "");
  if (m) from = new Date(to.getTime() - parseFloat(m[1]) * UNIT_MS[m[2]]);
  else if (r.from) from = new Date(r.from);
  else from = new Date(to.getTime() - 36e5);
  return { from, to };
}

export function rangeLabel(r: Range): string {
  const q = QUICK_RANGES.find((x) => x.from === r.from && !r.to);
  if (q) return `Last ${q.label}`;
  const { from, to } = resolveRange(r);
  return `${fmtShort(from)} → ${fmtShort(to)}`;
}

export function fmtTs(iso: string | Date, opts: { seconds?: boolean; ms?: boolean } = { seconds: true }): string {
  const d = typeof iso === "string" ? new Date(iso) : iso;
  if (isNaN(d.getTime())) return String(iso);
  const pad = (n: number, w = 2) => String(n).padStart(w, "0");
  let s = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  if (opts.seconds !== false) s += `:${pad(d.getSeconds())}`;
  if (opts.ms) s += `.${pad(d.getMilliseconds(), 3)}`;
  return s;
}

export function fmtShort(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  const today = new Date();
  const sameDay = d.toDateString() === today.toDateString();
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return sameDay ? hm : `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hm}`;
}

export function ago(iso: string, now = Date.now()): string {
  const t = new Date(iso).getTime();
  if (isNaN(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 5) return "just now";
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h ago`;
  const d = Math.round(h / 24);
  if (d < 60) return `${d}d ago`;
  return `${Math.round(d / 30)}mo ago`;
}

export function fmtDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${Math.round(ms / 60_000)}m`;
}

export function bucketLabel(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86400)}d`;
}

export function fmtAxisTime(d: Date, spanMs: number): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  if (spanMs > 14 * 864e5) return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  if (spanMs > 864e5) return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function toIso(d: Date): string {
  return d.toISOString();
}

export function fmtNumber(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e4) return `${(n / 1e3).toFixed(1)}k`;
  return n.toLocaleString();
}
