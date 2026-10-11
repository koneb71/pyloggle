# Changelog

## 0.2.0

A redesign of the server, the UI and the Python SDK. Projects already sending data need no
change; see `docs/upgrading-0.2.md`.

### Error reports

- Structured tracebacks: module, source context (5 lines before/after), in-app detection,
  local variables for in-app frames (names and values scrubbed), chained causes
  (`raise … from`, implicit context), exception groups and `__notes__`.
- Breadcrumbs (log lines, HTTP requests, manual), request (method, route, URL, query, scrubbed
  headers, client IP, body when enabled), user, runtime, SDK, release, host, tags and extra on
  every occurrence.
- Fingerprint v2 groups by exception type, module and the in-app call chain instead of
  `file:line`; custom fingerprints with `{{ default }}`; error-level logs without an exception
  group by logger and message template. The culprit is the innermost in-app frame.
- Regressions: an occurrence of a resolved issue reopens it and marks it regressed; alert
  conditions `new_issue` and `regression`.
- Issue counts, affected users and first/last seen are maintained by aggregating materialized
  views and are correct across all retained history (the previous per-batch stats were not).

### Ingest

- Lenient wire format v2: zone-less timestamps, unix seconds/ms, non-string attribute values and
  `critical`/`warning` levels are accepted instead of silently dropped. The ingest response
  reports `accepted`, `rejected` (with the first errors) and `dropped` by reason.
- OTLP/HTTP door: `POST /v1/logs`, `/v1/traces` (protobuf or JSON, gzip) authenticated with the
  project API key. Python tracebacks in `exception.stacktrace` are parsed into frames.
- Sampling keeps whole requests together (seeded by request id); defaults INFO 100 %, DEBUG 10 %.
- Embedded migrations run at startup; retention is a runtime setting applied with `MODIFY TTL`.
- Per-project quota, rate limits and drop counters on `/healthz?verbose=1`.

### Storage

- Daily partitions, `project_id`-first sort keys, ZSTD/Delta codecs, LowCardinality columns,
  bloom and token indexes. Expired data is dropped as whole parts (`ttl_only_drop_parts`).
- 30-day default retention for logs and errors, 365 days for the tiny aggregate tables.
- Tuned ClickHouse configuration mounted by compose and memory/CPU limits for both containers
  (`docs/footprint.md`).

### UI

- New single-page application (React, embedded in the binary) with light and dark themes.
- Logs explorer: query language (`service:api level:error "timed out" -attrs.path:/health`),
  facets with counts, stacked timeline with drag-to-zoom, patterns view, virtualized list with
  a column picker, detail panel with filter/exclude/copy on every value, same-request and
  same-trace links, view-in-context, live tail and saved views.
- Issues: status tabs with counts, search, sort, sparklines, bulk actions; detail with the
  exception chain, breadcrumbs, request, user and runtime, tags and attributes, related logs,
  raw JSON, breakdowns and older/newer navigation.
- Dashboards (stat tiles, error trend, log volume by level, top issues), Alerts and Projects.
- JSON API under `/ui/api/*` for the UI and `/v1/q/*` for scripts (API key).

### Python SDK 0.2

- Backward compatible with 0.1 call sites. New: `pyloggle.init()`, capture API, breadcrumbs,
  scopes, user/tags/context, `capture_errors`, global exception hooks (installed by default),
  asyncio hook, `flush`/`close`.
- Transport: gzip, retries with backoff and `Retry-After`, 413 splitting, oversize degradation,
  bounded queue, `flush()` waits for in-flight batches.
- Flask and FastAPI/ASGI integrations: one error event per failing request, request scope reset,
  `X-Request-ID` and `traceparent` honoured, real status codes.

### Fixed

- stdlib-logging events were dropped by the server (zone-less timestamps, boolean attributes).
- `warn` filters never matched `warning` rows; the "older" cursor ignored unix-ms timestamps.
- Alert rule filters produced invalid SQL; a race in the tenant resolver; dashboards chart was
  invisible; `.gitignore` ignored every `dist/`.

## 0.1.0

Initial release: Go `ingestd` with ClickHouse, server-rendered UI, Python SDK with Flask and
FastAPI integrations, threshold alerting.
