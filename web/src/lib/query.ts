// Client-side helpers for the log query language (chips + toggling terms). The server is the
// source of truth for parsing; this mirrors tokenization closely enough for UI editing.

export interface Chip {
  raw: string;
  negate: boolean;
  field: string | null; // null = free text
  value: string;
}

export function tokenize(q: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quote: string | null = null;
  let depth = 0;
  for (let i = 0; i < q.length; i++) {
    const c = q[i];
    if (quote) {
      cur += c;
      if (c === "\\" && i + 1 < q.length) {
        cur += q[++i];
        continue;
      }
      if (c === quote) quote = null;
      continue;
    }
    if (c === '"' || c === "'") {
      quote = c;
      cur += c;
      continue;
    }
    if (c === "(") depth++;
    if (c === ")" && depth > 0) depth--;
    if (/\s/.test(c) && depth === 0) {
      if (cur) out.push(cur);
      cur = "";
      continue;
    }
    cur += c;
  }
  if (cur) out.push(cur);
  return out;
}

export function chips(q: string): Chip[] {
  return tokenize(q)
    .filter((t) => t.toUpperCase() !== "AND")
    .map((t) => {
      const negate = t.startsWith("-") && t.length > 1 && !t.startsWith('-"');
      const body = negate ? t.slice(1) : t;
      const colon = body.startsWith('"') || body.startsWith("'") ? -1 : body.indexOf(":");
      if (colon > 0) return { raw: t, negate, field: body.slice(0, colon), value: body.slice(colon + 1) };
      return { raw: t, negate, field: null, value: body };
    });
}

export function quoteValue(v: string): string {
  if (v === "") return '""';
  if (/[\s"'()]/.test(v)) return `"${v.replace(/"/g, '\\"')}"`;
  return v;
}

export function term(field: string, value: string, negate = false): string {
  return `${negate ? "-" : ""}${field}:${quoteValue(value)}`;
}

/** Add a field:value term unless it is already present. */
export function addTerm(q: string, field: string, value: string, negate = false): string {
  const t = term(field, value, negate);
  const toks = tokenize(q);
  if (toks.includes(t)) return q;
  const opposite = term(field, value, !negate);
  const kept = toks.filter((x) => x !== opposite);
  return [...kept, t].join(" ");
}

export function removeTerm(q: string, raw: string): string {
  const toks = tokenize(q);
  const i = toks.indexOf(raw);
  if (i < 0) return q;
  toks.splice(i, 1);
  // drop a dangling OR
  const cleaned = toks.filter((t, idx) => !(t.toUpperCase() === "OR" && (idx === 0 || idx === toks.length - 1 || toks[idx - 1]?.toUpperCase() === "OR")));
  return cleaned.join(" ");
}

export function hasTerm(q: string, field: string, value: string): boolean {
  return tokenize(q).includes(term(field, value));
}
