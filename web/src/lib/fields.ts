/**
 * Field names → human labels, and the rule for when a value is code (monospace) rather than
 * prose. Used by filters, columns, key/value lists, charts, the alert form and search help, so
 * "http.status" reads as "Response code" everywhere while the query language keeps its names.
 */

const LABELS: Record<string, string> = {
  ts: "Time",
  timestamp: "Time",
  time: "Time",
  level: "Severity",
  service: "App",
  env: "Environment",
  environment: "Environment",
  release: "Version",
  host: "Server",
  hostname: "Server",
  logger: "Code module",
  message: "Message",
  msg: "Message",
  source: "Sent by",
  project_id: "Project ID",
  event_id: "Event ID",
  issue_id: "Issue ID",
  trace_id: "Request timeline ID",
  span_id: "Step ID",
  parent_span_id: "Parent step ID",
  request_id: "Request ID",
  duration_ms: "Duration",
  duration: "Duration",
  kind: "Kind",
  status: "Status",
  name: "Name",
  exception: "Error type",
  exception_type: "Error type",
  exception_message: "Error message",
  culprit: "Where it happened",
  fingerprint: "Grouping key",
  stack_fingerprint: "Grouping key",
  user_agent: "Browser",
  ip: "IP address",
  remote_addr: "Remote address",
  id_hash: "Person (anonymized)",
  user_id_hash: "Person (anonymized)",
  "user.id": "User ID",
  "user.id_hash": "Person (anonymized)",
  "user.email": "E-mail",
  "user.username": "Username",
  "user.ip": "IP address",
  "http.method": "Method",
  http_method: "Method",
  "http.route": "Route",
  http_route: "Route",
  "http.path": "Path",
  "http.url": "URL",
  http_url: "URL",
  "http.status": "Response code",
  http_status: "Response code",
  "http.status_code": "Response code",
  "http.query": "Query string",
  "http.headers": "Headers",
  "http.client_ip": "IP address",
  "http.user_agent": "Browser",
  "runtime.name": "Runtime",
  "runtime.version": "Runtime version",
  "runtime.platform": "Platform",
  "runtime.pid": "Process ID",
  "runtime.thread": "Thread",
  "sdk.name": "SDK",
  "sdk.version": "SDK version",
  "code.function": "Function",
  "code.filepath": "File",
  "code.lineno": "Line",
  "code.namespace": "Module",
  "db.system": "Database",
  "db.statement": "Database query",
  "db.name": "Database name",
  "process.pid": "Process ID",
  "thread.name": "Thread",
  "mechanism.type": "Caught by",
  "mechanism.handled": "Handled",
  "service.name": "App",
  "service.version": "Version",
  "deployment.environment": "Environment",
  "deployment.environment.name": "Environment",
  "host.name": "Server",
  "server.address": "Server address",
  "url.full": "URL",
  "url.path": "Path",
  "http.request.method": "Method",
  "http.response.status_code": "Response code",
};

const ACRONYMS: Record<string, string> = {
  id: "ID",
  ids: "IDs",
  url: "URL",
  uri: "URI",
  ip: "IP",
  http: "HTTP",
  https: "HTTPS",
  sql: "SQL",
  db: "Database",
  api: "API",
  os: "OS",
  sdk: "SDK",
  cpu: "CPU",
  ms: "(ms)",
  ttl: "TTL",
  pid: "Process ID",
  uuid: "UUID",
  json: "JSON",
  utc: "UTC",
};

const PREFIXES = ["attrs.", "attr.", "attributes.", "tags.", "tag.", "labels.", "resource."];

const SOURCE_WORDS: Record<string, string> = { sdk: "Pyloggle SDK", otlp: "OpenTelemetry", http: "HTTP API", syslog: "Syslog" };

/** How the data arrived, in words: "sdk" → "Pyloggle SDK". */
export function sourceLabel(source?: string): string {
  if (!source) return "";
  return SOURCE_WORDS[source.toLowerCase()] ?? source;
}

/** A human label for a field: "http.status" → "Response code", "order_id" → "Order ID". */
export function fieldLabel(key: string): string {
  if (!key) return "";
  if (LABELS[key]) return LABELS[key];
  for (const p of PREFIXES) {
    if (key.startsWith(p)) return fieldLabel(key.slice(p.length));
  }
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .split(/[._\-\s]+/)
    .filter(Boolean)
    .map((w) => w.toLowerCase());
  if (words.length === 0) return key;
  const out = words.map((w) => ACRONYMS[w] ?? w);
  out[0] = out[0].charAt(0).toUpperCase() + out[0].slice(1);
  return out.join(" ");
}

const CODE_KEY = /(^|[._])(id|ids|hash|trace_id|span_id|event_id|request_id|fingerprint|path|filepath|file|filename|function|func|module|namespace|logger|statement|query|sql|stacktrace|stack|url|uri|route|user_agent|sha|commit|key|token|uuid)$/i;
const CODE_KEYS = new Set(["exception", "exception_type", "culprit", "code.lineno", "http.method", "http_method", "runtime.pid", "process.pid", "id_hash"]);
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const LONG_HEX = /^[0-9a-f]{12,}$/i;
const PATHLIKE = /^(\/|[a-z]:\\|\.{1,2}\/)[^\s]*$/i;

/**
 * True when a field's value is code or an identifier, so it should be set in monospace:
 * ids, hashes, paths, routes, files, functions, modules, queries. Values that look like
 * UUIDs, long hex or file paths count too, whatever their key.
 */
export function isCodeLike(key: string, value?: string): boolean {
  const k = key.replace(/^(attrs|attr|attributes|tags|tag|labels|resource)\./, "");
  if (CODE_KEYS.has(k) || CODE_KEY.test(k)) return true;
  if (value === undefined) return false;
  const v = value.trim();
  return UUID.test(v) || LONG_HEX.test(v) || PATHLIKE.test(v);
}
