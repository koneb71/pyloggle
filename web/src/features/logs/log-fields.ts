import type { LogEvent } from "@/api/types";
import { fieldLabel, isCodeLike, sourceLabel } from "@/lib/fields";

/** Facet (ClickHouse column) → query-language field. */
export const FACET_TO_FIELD: Record<string, string> = {
  level: "level",
  service: "service",
  env: "env",
  logger: "logger",
  hostname: "host",
  release: "release",
  http_method: "http.method",
  http_status: "http.status",
  http_route: "http.route",
  source: "source",
  exception_type: "exception",
};

/** Filter groups in the sidebar, by what people look for. Labels come from fieldLabel. */
export const FACET_GROUPS: { title: string; facets: string[] }[] = [
  { title: "General", facets: ["level", "service", "env", "logger", "hostname", "release"] },
  { title: "Web requests", facets: ["http_method", "http_status", "http_route"] },
];

/** Filters open by default; the rest start collapsed to keep the sidebar calm. */
export const OPEN_FACETS = new Set(["level", "service"]);

export interface ColumnDef {
  id: string; // "service" | ... | "attrs.<key>" | "tags.<key>"
  label: string;
  /** The field name engineers know (tooltip on the header). */
  technical: string;
  width: string; // CSS grid track
  value: (e: LogEvent) => string;
  mono?: boolean;
  align?: "right";
}

const col = (id: string, technical: string, width: string, value: (e: LogEvent) => string, extra: Partial<ColumnDef> = {}): ColumnDef => ({ id, technical, label: fieldLabel(technical), width, value, ...extra });

export const BUILTIN_COLUMNS: ColumnDef[] = [
  col("service", "service", "minmax(90px,150px)", (e) => e.service),
  col("env", "env", "100px", (e) => e.env),
  col("logger", "logger", "minmax(100px,170px)", (e) => e.logger, { mono: true }),
  col("host", "host", "minmax(90px,140px)", (e) => e.hostname ?? ""),
  col("http", "http.route", "minmax(140px,240px)", (e) => (e.http_method || e.http_route ? `${e.http_method ?? ""} ${e.http_route ?? ""} ${e.http_status ? e.http_status : ""}`.trim() : ""), { mono: true, label: "Request" }),
  col("duration", "duration_ms", "80px", (e) => (e.duration_ms ? `${e.duration_ms.toFixed(e.duration_ms < 10 ? 1 : 0)} ms` : ""), { align: "right" }),
  col("request_id", "request_id", "120px", (e) => e.request_id, { mono: true }),
  col("trace_id", "trace_id", "120px", (e) => e.trace_id, { mono: true }),
  col("release", "release", "100px", (e) => e.release ?? ""),
  col("source", "source", "100px", (e) => sourceLabel(e.source)),
];

export function columnFor(id: string): ColumnDef | undefined {
  const b = BUILTIN_COLUMNS.find((c) => c.id === id);
  if (b) return b;
  if (id.startsWith("attrs.")) {
    const k = id.slice(6);
    return col(id, id, "minmax(90px,160px)", (e) => e.attrs?.[k] ?? "", { mono: isCodeLike(k) });
  }
  if (id.startsWith("tags.")) {
    const k = id.slice(5);
    return col(id, id, "minmax(80px,140px)", (e) => e.tags?.[k] ?? "", { mono: isCodeLike(k) });
  }
  return undefined;
}

export const DEFAULT_COLUMNS = ["service"];

/** Extract a quotable literal phrase from a pattern template ("<n>", "<s>", "<hex>", "<uuid>" placeholders). */
export function patternPhrase(pattern: string): string {
  const parts = pattern
    .split(/<(?:n|s|hex|uuid)>/)
    .map((p) => p.trim())
    .filter((p) => p.split(/\s+/).filter(Boolean).length >= 2 || p.length >= 12);
  parts.sort((a, b) => b.length - a.length);
  return parts[0] ?? pattern.trim();
}
