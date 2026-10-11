import type { LogEvent } from "@/api/types";

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

export const FACET_GROUPS: { title: string; facets: { key: string; label: string }[] }[] = [
  {
    title: "Core",
    facets: [
      { key: "level", label: "Level" },
      { key: "service", label: "Service" },
      { key: "env", label: "Env" },
      { key: "logger", label: "Logger" },
      { key: "hostname", label: "Host" },
      { key: "release", label: "Release" },
    ],
  },
  {
    title: "HTTP",
    facets: [
      { key: "http_method", label: "Method" },
      { key: "http_status", label: "Status" },
      { key: "http_route", label: "Route" },
    ],
  },
];

export interface ColumnDef {
  id: string; // "time" | "level" | ... | "attrs.<key>" | "tags.<key>"
  label: string;
  width: string; // CSS grid track
  value: (e: LogEvent) => string;
  mono?: boolean;
  align?: "right";
}

const col = (id: string, label: string, width: string, value: (e: LogEvent) => string, extra: Partial<ColumnDef> = {}): ColumnDef => ({ id, label, width, value, ...extra });

export const BUILTIN_COLUMNS: ColumnDef[] = [
  col("service", "Service", "minmax(90px,140px)", (e) => e.service),
  col("env", "Env", "80px", (e) => e.env),
  col("logger", "Logger", "minmax(100px,160px)", (e) => e.logger, { mono: true }),
  col("host", "Host", "minmax(90px,140px)", (e) => e.hostname ?? "", { mono: true }),
  col("http", "HTTP", "minmax(120px,220px)", (e) => (e.http_method || e.http_route ? `${e.http_method ?? ""} ${e.http_route ?? ""} ${e.http_status ? e.http_status : ""}`.trim() : ""), { mono: true }),
  col("duration", "Duration", "70px", (e) => (e.duration_ms ? `${e.duration_ms.toFixed(e.duration_ms < 10 ? 2 : 0)}ms` : ""), { align: "right", mono: true }),
  col("request_id", "Request", "110px", (e) => e.request_id, { mono: true }),
  col("trace_id", "Trace", "110px", (e) => e.trace_id, { mono: true }),
  col("release", "Release", "90px", (e) => e.release ?? ""),
  col("source", "Source", "80px", (e) => e.source ?? ""),
];

export function columnFor(id: string): ColumnDef | undefined {
  const b = BUILTIN_COLUMNS.find((c) => c.id === id);
  if (b) return b;
  if (id.startsWith("attrs.")) {
    const k = id.slice(6);
    return col(id, k, "minmax(90px,160px)", (e) => e.attrs?.[k] ?? "", { mono: true });
  }
  if (id.startsWith("tags.")) {
    const k = id.slice(5);
    return col(id, `#${k}`, "minmax(80px,140px)", (e) => e.tags?.[k] ?? "", { mono: true });
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
