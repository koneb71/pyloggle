import type { Span } from "@/api/types";

export interface WaterfallRow {
  span: Span;
  depth: number;
  /** Offset and width as a percentage of the trace duration. */
  left: number;
  width: number;
  /** Direct children count; 0 for leaves. */
  children: number;
  /** Spans in the subtree (excluding this one). */
  descendants: number;
  /** True when this span or any descendant has status error. */
  hasError: boolean;
  startUs: number;
  endUs: number;
}

const MIN_WIDTH_PCT = 0.3;

/**
 * Orders spans for the waterfall: depth-first by start time, orphans (parent not in the trace)
 * at depth 0. Offsets are relative to the earliest span; widths never collapse below a sliver
 * so sub-millisecond spans stay clickable.
 */
export function layoutSpans(spans: Span[]): WaterfallRow[] {
  if (spans.length === 0) return [];
  const byId = new Map(spans.map((s) => [s.span_id, s]));
  const startUs = (s: Span) => new Date(s.start_ts).getTime() * 1000 + microFraction(s.start_ts);
  const t0 = Math.min(...spans.map(startUs));
  const t1 = Math.max(...spans.map((s) => startUs(s) + s.duration_us));
  const total = Math.max(1, t1 - t0);

  const children = new Map<string, Span[]>();
  const roots: Span[] = [];
  for (const s of spans) {
    const p = s.parent_span_id;
    if (p && byId.has(p) && p !== s.span_id) {
      const list = children.get(p) ?? [];
      list.push(s);
      children.set(p, list);
    } else roots.push(s);
  }
  const byStart = (a: Span, b: Span) => startUs(a) - startUs(b) || a.span_id.localeCompare(b.span_id);
  roots.sort(byStart);
  for (const list of children.values()) list.sort(byStart);

  const out: WaterfallRow[] = [];
  const visiting = new Set<string>();
  const walk = (s: Span, depth: number): { count: number; error: boolean } => {
    if (visiting.has(s.span_id)) return { count: 0, error: false };
    visiting.add(s.span_id);
    const st = startUs(s) - t0;
    const row: WaterfallRow = {
      span: s,
      depth,
      left: (st / total) * 100,
      width: Math.max(MIN_WIDTH_PCT, (s.duration_us / total) * 100),
      children: (children.get(s.span_id) ?? []).length,
      descendants: 0,
      hasError: s.status === "error",
      startUs: st,
      endUs: st + s.duration_us,
    };
    if (row.left + row.width > 100) row.width = Math.max(MIN_WIDTH_PCT, 100 - row.left);
    out.push(row);
    let count = 0;
    let error = row.hasError;
    for (const c of children.get(s.span_id) ?? []) {
      const r = walk(c, depth + 1);
      count += 1 + r.count;
      error = error || r.error;
    }
    row.descendants = count;
    row.hasError = error;
    return { count, error };
  };
  for (const r of roots) walk(r, 0);
  // Spans unreachable from a root (cycles) still show up, flat.
  for (const s of spans) if (!visiting.has(s.span_id)) walk(s, 0);
  return out;
}

/** Rows after applying collapsed subtrees (a collapsed span hides its descendants). */
export function visibleRows(rows: WaterfallRow[], collapsed: ReadonlySet<string>): WaterfallRow[] {
  const out: WaterfallRow[] = [];
  let skipUntilDepth = -1;
  for (const r of rows) {
    if (skipUntilDepth >= 0 && r.depth > skipUntilDepth) continue;
    skipUntilDepth = -1;
    out.push(r);
    if (collapsed.has(r.span.span_id) && r.children > 0) skipUntilDepth = r.depth;
  }
  return out;
}

/** Microseconds below the millisecond in an RFC3339 timestamp ("…:20.123456Z" → 456). */
export function microFraction(iso: string): number {
  const m = /\.(\d{3})(\d{1,3})/.exec(iso);
  if (!m) return 0;
  return Number(m[2].padEnd(3, "0"));
}

/** Human duration from microseconds. */
export function fmtUs(us: number): string {
  if (us === 0) return "0ms";
  if (us < 1000) return `${Math.round(us)}µs`;
  if (us < 10_000) return `${(us / 1000).toFixed(2)}ms`;
  if (us < 1_000_000) return `${(us / 1000).toFixed(us < 100_000 ? 1 : 0)}ms`;
  if (us < 60_000_000) return `${(us / 1_000_000).toFixed(2)}s`;
  return `${(us / 60_000_000).toFixed(1)}m`;
}

/** Tick positions (percent, label) for the time ruler above the waterfall. */
export function rulerTicks(totalUs: number, count = 5): { pct: number; label: string }[] {
  const out: { pct: number; label: string }[] = [];
  for (let i = 0; i <= count; i++) {
    const pct = (i / count) * 100;
    out.push({ pct, label: fmtUs((totalUs * i) / count) });
  }
  return out;
}
