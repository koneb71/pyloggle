/**
 * Plain-language vocabulary. Every user-facing term goes through here so the whole UI speaks
 * the same words: a plain label first, the technical name (what engineers and docs call it)
 * second, and one sentence explaining it. `<Term>` renders the label with this as its tooltip.
 */
export interface GlossaryEntry {
  /** What the UI shows. */
  label: string;
  /** What engineers, docs and the query language call it. */
  technical: string;
  /** One sentence for someone who has never heard the technical name. */
  explain: string;
}

const g = (label: string, technical: string, explain: string): GlossaryEntry => ({ label, technical, explain });

export const GLOSSARY = {
  issue: g("Issue", "issue", "Errors with the same cause, grouped together so you fix each problem once."),
  occurrence: g("Time it happened", "occurrence (event)", "One single time the error happened, with everything recorded at that moment."),
  events: g("Times", "events", "How many times the error happened."),
  users: g("People affected", "users", "How many different signed-in people hit the error."),
  unresolved: g("Unresolved", "open", "Still happening, or not yet marked as fixed."),
  regressed: g("Came back", "regressed", "Marked as fixed, then happened again."),
  resolved: g("Resolved", "resolved", "Marked as fixed. You'll be told if it happens again."),
  ignored: g("Ignored", "ignored", "Hidden from the list on purpose. It is still recorded."),
  unhandled: g("Crashed", "unhandled", "Nothing in the app caught this error, so the request or job failed."),
  culprit: g("Where it happened", "culprit", "The function and file in your code where the error was raised."),
  breadcrumbs: g("Steps before the error", "breadcrumbs", "What the app did just before the error: queries, outgoing calls, log lines."),
  release: g("Version", "release", "The version of your app that was running, as reported by the SDK."),
  env: g("Environment", "env", "Where the app was running, for example production or staging."),
  service: g("App", "service", "The name your application reports itself as."),
  host: g("Server", "host", "The machine or container that sent the data."),
  logger: g("Code module", "logger", "The part of your code that wrote the message."),
  level: g("Severity", "level", "How serious a message is: debug, info, warning, error or fatal."),
  p50: g("Typical", "p50 (median)", "Half of the requests were faster than this."),
  p95: g("Slow", "p95", "The slowest 5% of requests took at least this long."),
  p99: g("Slowest", "p99", "The slowest 1% of requests took at least this long."),
  failed: g("Failed requests", "5xx responses", "Requests that ended with a server error (a 5xx response code)."),
  trace: g("Request timeline", "trace", "Everything one request did, step by step, across all your apps."),
  span: g("Step", "span", "One timed piece of work inside a request, like a database query or an outgoing call."),
  facets: g("Filters", "facets", "The most common values of each field, so you can narrow the list with one click."),
  patterns: g("Similar messages", "patterns", "Messages grouped by their shape, with the changing parts (numbers, ids) blanked out."),
  analytics: g("Charts", "analytics", "Count or measure messages, errors or requests over time, split by a field."),
  people: g("People", "unique users", "How many different signed-in people, from the hashed ID your app sends. An estimate, never a list of who."),
  cooldown: g("Wait before alerting again", "cooldown", "After an alert is sent, stay quiet for this long even if the condition is still true."),
  threshold: g("Limit", "threshold", "The number the alert compares against."),
  scopes: g("Permissions", "scopes", "What an access key is allowed to do."),
  ingest: g("Send data", "ingest scope", "The key can send logs, errors and traces to this project."),
  read: g("View", "read scope", "The key can read data, for scripts and the query API."),
  write: g("Make changes", "write scope", "The key can change settings such as alert rules and issue states."),
  inApp: g("Your code", "in-app frames", "Lines from your own project, as opposed to libraries."),
  frames: g("Code steps", "stack frames", "The chain of function calls that led to the error."),
  requestsPerMin: g("Requests per minute", "throughput", "How many requests the app handled each minute."),
  sampled: g("Sampled", "sampling", "Only a share of these messages is kept to save space."),
} satisfies Record<string, GlossaryEntry>;

export type TermKey = keyof typeof GLOSSARY;

export function term(key: TermKey): GlossaryEntry {
  return GLOSSARY[key];
}

/** "Came back (regressed)": the plain label with the technical name, for places with room for both. */
export function labelWithTechnical(key: TermKey): string {
  const e = GLOSSARY[key];
  return e.label.toLowerCase() === e.technical.toLowerCase() ? e.label : `${e.label} (${e.technical})`;
}
