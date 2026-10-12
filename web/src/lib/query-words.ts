import type { Chip } from "./query";
import { fieldLabel } from "./fields";
import { levelLabel } from "./levels";

function unquote(v: string): string {
  if (v.length >= 2 && (v[0] === '"' || v[0] === "'") && v[v.length - 1] === v[0]) return v.slice(1, -1).replace(/\\(["'])/g, "$1");
  return v;
}

function valueWords(field: string, v: string): string {
  const u = unquote(v);
  if (field === "level") return levelLabel(u);
  if (u === "") return "empty";
  return u;
}

const COMPARE: [RegExp, string][] = [
  [/^>=(.*)$/, "≥"],
  [/^<=(.*)$/, "≤"],
  [/^>(.*)$/, ">"],
  [/^<(.*)$/, "<"],
];

/**
 * One search term in words, for the chips under the search box: "service:api" → "App is api",
 * "-env:dev" → "Environment is not dev", "http.status:>=500" → "Response code ≥ 500",
 * '"timed out"' → 'Message contains "timed out"'. The query text stays the source of truth.
 */
export function describeChip(c: Chip): string {
  if (!c.field) {
    const text = unquote(c.value);
    if (/^(OR|AND|NOT)$/i.test(c.value)) return c.value.toUpperCase();
    return c.negate ? `Message doesn't contain "${text}"` : `Message contains "${text}"`;
  }
  const field = c.field.startsWith("@") ? `attrs.${c.field.slice(1)}` : c.field;
  const label = fieldLabel(field);
  const v = c.value;
  // level:(error OR fatal)
  const group = /^\((.*)\)$/.exec(v);
  if (group) {
    const parts = group[1]
      .split(/\s+OR\s+/i)
      .map((x) => valueWords(field, x.trim()))
      .filter(Boolean);
    const list = parts.length > 1 ? `${parts.slice(0, -1).join(", ")} or ${parts[parts.length - 1]}` : parts[0] ?? "";
    return `${label} ${c.negate ? "is not" : "is"} ${list}`;
  }
  for (const [re, sym] of COMPARE) {
    const m = re.exec(v);
    if (m) return `${c.negate ? "not " : ""}${label} ${sym} ${unquote(m[1])}`;
  }
  const u = unquote(v);
  if (u.endsWith("*") && !u.startsWith('"')) return `${label} ${c.negate ? "doesn't start" : "starts"} with ${u.slice(0, -1)}`;
  return `${label} ${c.negate ? "is not" : "is"} ${valueWords(field, v)}`;
}
