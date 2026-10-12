import type { Span, TraceSummary } from "@/api/types";
import type { SearchExample } from "@/features/logs/search-bar";

/** "How to search" for requests. */
export const TRACE_SEARCH_EXAMPLES: SearchExample[] = [
  { query: '"GET /orders"', says: "Requests and steps with this name" },
  { query: "service:api", says: "Only one app" },
  { query: "status:error", says: "Only steps that failed" },
  { query: "duration:>250", says: "Steps slower than 250 ms" },
  { query: "kind:client", says: "Calls to other services" },
  { query: "db.system:postgresql", says: "Database steps" },
  { query: "-service:worker", says: "Leave an app out (a minus in front)" },
];

export type RequestResult = "failed" | "ok" | "unknown";

/** A request's outcome in one word: failed if any step failed or the response was a 5xx. */
export function requestResult(t: Pick<TraceSummary, "status" | "errors" | "http_status">): RequestResult {
  if (t.status === "error" || t.errors > 0 || (t.http_status ?? 0) >= 500) return "failed";
  if (t.status === "ok" || (t.http_status ?? 0) > 0) return "ok";
  return "unknown";
}

/** "GET /orders" when it is a web request, otherwise the step's name. */
export function requestLabel(s: Pick<Span | TraceSummary, "http_method" | "http_route" | "name">): string {
  return s.http_method && s.http_route ? `${s.http_method} ${s.http_route}` : s.name;
}

const KIND_WORDS: Record<string, string> = {
  server: "Handles a request",
  client: "Calls another service",
  internal: "Work inside the app",
  producer: "Sends a message",
  consumer: "Receives a message",
};

/** OpenTelemetry span kinds in words. */
export function kindWords(kind?: string): string {
  if (!kind) return "";
  return KIND_WORDS[kind.toLowerCase()] ?? kind;
}

/** Span status in words: OK, Failed, or "Not reported" for OpenTelemetry's unset. */
export function statusWords(status?: string): string {
  if (status === "error") return "Failed";
  if (status === "ok") return "OK";
  return "Not reported";
}
