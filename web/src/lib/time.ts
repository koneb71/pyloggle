export interface Range {
  from: string; // relative ("-1h") or absolute ISO
  to: string; // "" = now, or absolute ISO
}

export const QUICK_RANGES: { label: string; short: string; from: string }[] = [
  { label: "15 minutes", short: "15m", from: "-15m" },
  { label: "hour", short: "1h", from: "-1h" },
  { label: "4 hours", short: "4h", from: "-4h" },
  { label: "24 hours", short: "24h", from: "-24h" },
  { label: "2 days", short: "2d", from: "-2d" },
  { label: "7 days", short: "7d", from: "-7d" },
  { label: "30 days", short: "30d", from: "-30d" },
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

const UNIT_WORD: Record<string, string> = { s: "second", m: "minute", h: "hour", d: "day", w: "week" };

/** "-3d" → "3 days", "-1h" → "hour": a relative range's length in words, or "" if not relative. */
function relativeWords(from: string): string {
  const q = QUICK_RANGES.find((x) => x.from === from);
  if (q) return q.label;
  const m = RELATIVE.exec(from || "");
  if (!m) return "";
  const n = parseFloat(m[1]);
  const w = UNIT_WORD[m[2]];
  return n === 1 ? w : `${m[1]} ${w}s`;
}

export function rangeLabel(r: Range): string {
  const words = !r.to ? relativeWords(r.from) : "";
  if (words) return `Last ${words}`;
  const { from, to } = resolveRange(r);
  return `${fmtShort(from)} – ${fmtShort(to)}`;
}

/** "the last 24 hours" for a recent range, or the dates for a fixed one: reads inside a sentence. */
export function rangePhrase(r: Range): string {
  const words = !r.to ? relativeWords(r.from) : "";
  return words ? `the last ${words}` : rangeLabel(r);
}

/** "vs previous 24 hours": the label for a change against the same-length range before. */
export function previousLabel(r: Range): string {
  const words = !r.to ? relativeWords(r.from) : "";
  return words ? `vs previous ${words}` : "vs the period before";
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
  if (s < 10) return "just now";
  if (s < 60) return `${s} sec ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 48) return h === 1 ? "1 hour ago" : `${h} hours ago`;
  const d = Math.round(h / 24);
  if (d < 60) return `${d} days ago`;
  const mo = Math.round(d / 30);
  return mo === 1 ? "1 month ago" : `${mo} months ago`;
}

/** "Today 09:28:56", "Yesterday 18:02", "Oct 3 18:02": readable times for tables. */
export function fmtWhen(iso: string | Date, opts: { seconds?: boolean } = {}, now = new Date()): string {
  const d = typeof iso === "string" ? new Date(iso) : iso;
  if (isNaN(d.getTime())) return String(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}${opts.seconds ? `:${pad(d.getSeconds())}` : ""}`;
  const day = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const that = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  if (that === day) return `Today ${hm}`;
  if (that === day - 864e5) return `Yesterday ${hm}`;
  const md = d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  return d.getFullYear() === now.getFullYear() ? `${md} ${hm}` : `${md}, ${d.getFullYear()} ${hm}`;
}

/** "1 hour", "5 minutes": a bucket or window length in words. */
export function durationWords(seconds: number): string {
  const unit = (n: number, w: string) => `${n} ${w}${n === 1 ? "" : "s"}`;
  if (seconds < 60) return unit(seconds, "second");
  if (seconds < 3600) return unit(Math.round(seconds / 60), "minute");
  if (seconds < 86400) return unit(Math.round(seconds / 3600), "hour");
  return unit(Math.round(seconds / 86400), "day");
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

/** Axis ticks: "Oct 5" for long spans, "Oct 5 12:00" across days, "12:00" within a day. */
export function fmtAxisTime(d: Date, spanMs: number): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  const md = d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  if (spanMs > 14 * 864e5) return md;
  if (spanMs > 864e5) return `${md} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function toIso(d: Date): string {
  return d.toISOString();
}

/** Short numbers for chart axes: 950, 1.5k, 60k, 2.4M. */
export function fmtAxisNumber(n: number): string {
  const a = Math.abs(n);
  if (a >= 1e6) return `${+(n / 1e6).toFixed(a >= 1e7 ? 0 : 1)}M`;
  if (a >= 1e3) return `${+(n / 1e3).toFixed(a >= 1e4 ? 0 : 1)}k`;
  return Number.isInteger(n) ? String(n) : n.toFixed(1);
}

export function fmtNumber(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e4) return `${(n / 1e3).toFixed(1)}k`;
  return n.toLocaleString();
}

/** Latency formatting: sub-10ms keeps a decimal, then whole ms, then seconds. */
export function fmtMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !isFinite(ms)) return "–";
  if (ms < 10) return `${ms.toFixed(1)}ms`;
  return fmtDuration(ms);
}

/** Compact percentage: 0.034 → "0.03%", 12.3 → "12.3%", 100 → "100%". */
export function fmtPct(pct: number): string {
  if (!isFinite(pct)) return "–";
  if (pct === 0) return "0%";
  if (pct >= 100) return "100%";
  if (pct >= 10) return `${pct.toFixed(1)}%`;
  return `${pct.toFixed(2)}%`;
}

/** Value formatter for an OpenTelemetry unit (s, ms, By, 1, {request}...). */
export function unitFormatter(unit?: string): (v: number) => string {
  switch (unit) {
    case "s":
      return (v) => fmtMs(v * 1000);
    case "ms":
      return (v) => fmtMs(v);
    case "us":
      return (v) => fmtMs(v / 1000);
    case "ns":
      return (v) => fmtMs(v / 1e6);
    case "By":
    case "by":
      return fmtBytes;
    case "%":
      return (v) => `${v.toFixed(1)}%`;
  }
  return (v) => (Number.isInteger(v) ? fmtNumber(v) : Math.abs(v) >= 100 ? fmtNumber(Math.round(v)) : v.toFixed(2));
}

export function fmtBytes(n: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = n;
  while (Math.abs(v) >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
