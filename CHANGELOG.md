# Changelog

## 1.0.0

The first open-source release. Pyloggle is now licensed under the Apache License 2.0, has a
documentation site, and is released as a Docker image, binaries and a Python package on PyPI.
It also brings a redesigned app for the whole team, not only engineers, alerts that say what
went wrong and where, each alert sent only once, and a recap of yesterday. One migration (016)
adds a table and two columns.

Read [upgrading to 1.0](docs/self-hosting/upgrading.md) before you upgrade: a server that
relied on the old sign-in default no longer starts until the mode is set.

### Open source

- Apache License 2.0 for the server, the web app and the Python package (which was marked MIT
  but never published). `NOTICE` and `THIRD_PARTY_LICENSES.md` list the Go and web
  dependencies the program includes; the app serves the same list at `/ui/licenses.txt`, and
  the Docker image carries the license files.
- A documentation site, [koneb71.github.io/pyloggle](https://koneb71.github.io/pyloggle/),
  built from `docs/`: an introduction, a guide to every page of the app with screenshots in
  light and dark, the Python package and its frameworks, OpenTelemetry, deployment, sign-in,
  alerting, security, upgrading, and references for the configuration, the HTTP API and the
  webhook payload. Tests fail when a setting, an API route, a webhook field, a Python package
  option or a word of the app's glossary is missing from them, or when the app links to a page
  that does not exist.
- Releases on version tags: the image `ghcr.io/koneb71/pyloggle` for amd64 and arm64 with
  provenance and an SBOM, binaries for Linux and macOS (amd64 and arm64) with checksums, and
  the Python package on PyPI through trusted publishing.
- Continuous integration on every push and pull request: Go tests with the race detector, the
  ClickHouse integration tests, the web app, the Python package on 3.9 and 3.13, a dry run of
  the release, and checks that the tree holds no caches or private paths and that the versions
  agree. Dependabot keeps dependencies current.
- A contributing guide, a code of conduct (Contributor Covenant 2.1), a security policy with
  private reporting, and issue and pull request templates.
- `make dev-db` starts ClickHouse for development, `make demo-data` fills a server with two days
  of realistic data, and `make docs-dev` and `make docs-screenshots` work on the documentation.
- Version 1.0.0 for the server, the Python package and the web app. `ingestd -version` prints
  it, and the server logs it when it starts.

### Breaking

See [upgrading to 1.0](docs/self-hosting/upgrading.md) for what to do about each.

- **The sign-in mode must be chosen.** Without `PYLOGGLE_UI_AUTH`, 0.3 sent everyone to a
  Microsoft sign-in, and an unknown value did the same. 1.0 infers `azure` from
  `PYLOGGLE_AZURE_TENANT_ID` or `oidc` from `PYLOGGLE_OIDC_ISSUER`; with neither, both, or an
  unknown value, the server does not start and says what to set. Missing sign-in settings and
  a bad session secret are reported at start, all at once, instead of at the first sign-in.
- **Reading data needs a key while sign-in is on.** `/v1/q` and the 0.1 read endpoints answer
  `401 api_key_required` to a request without an access key. Sending data without a key is
  unchanged.
- **An empty `PYLOGGLE_DEFAULT_PROJECT_ID` means no default project**, so every request needs a
  key. 0.3 ignored the empty value and used `default`.
- **Docker Compose publishes the port on `127.0.0.1` only** and runs the published image
  (`PYLOGGLE_VERSION` pins one), building from source only when the image is not available.
- `PYLOGGLE_UI_POLL_SECONDS` is removed; it had no effect since 0.2.
- The Go module is `github.com/koneb71/pyloggle`.

### Look and language

- Calm product look: Inter, white cards on a warm gray page, one blue accent, sentence-case
  labels, roomier rows, and monospace only for code, ids and queries. Text and accent colors
  meet WCAG AA (4.5:1) in light and dark.
- Plain words first, the technical name second, from one glossary (`web/src/lib/glossary.ts`):
  "Came back (regressed)", "Slow (p95)", "Request timeline (trace)", "Steps before the error
  (breadcrumbs)". Field names read as words ("Response code", not `http.status`). A test
  fails if internal names such as `request_stats_minutely` reach the UI again.
- Every page has a one-line description; tables share one column grid so headers line up;
  each view shows exactly one of loading, error, empty or data; failed actions show a toast.
- The sidebar groups pages (Investigate, Monitor, Settings) and shrinks to icons below
  1200 px; `?` lists the keyboard shortcuts; logs, filters and details fit at 1024 px.

### New

- **Overview** home page (`/ui/` opens it): a status line, five headline numbers with the
  change against the previous period, issues that are new, came back or jumped, each app's
  health (OK, degraded, failing or silent), recent versions and recent alerts. Backed by
  `GET metrics/services`, `metrics/attention` and `releases`, and `compare=previous` on
  `metrics/overview` and `metrics/requests`.
- Logs: an *Add filter* picker, plain-language search chips and help with examples, a
  filters sidebar, a chart legend above the plot with the bar width spelled out.
- Alerts: rules read and are edited as sentences, with a *Test this alert* button.
- Projects: a *Connect your app* guide (Python, Django, FastAPI, OpenTelemetry) with the
  server's address filled in; settings, keys and storage in a side drawer.
- **Help** page (`g h`, at the bottom of the sidebar): guides from the documentation, the
  *Connect your app* steps, search examples you can click, every keyboard shortcut, the words
  the app uses and what engineers call them, and this server's version and sign-in. Alerts,
  Projects, System and the search help link to the matching documentation page.
  `PYLOGGLE_DOCS_URL` points those links at an internal copy of the docs, or hides them when
  empty; `/me` now reports `version` and `features.docs_url`.

### Alerts with context

- Every notification (e-mail, Telegram, Slack) is written from one message in plain words: what
  happened ("PaymentDeclined happened 60 times in 5 minutes, up from 3"), where ("api · prod",
  or "all apps · all environments") and when ("Oct 11, 11:13–11:18 UTC (5 minutes), compared
  with the 5 minutes before").
- For the first three problems: how many times and how many people, the file, line and code
  that failed, version and route, the last three steps before the error (URLs without their
  query strings), and links to that exact time, the request's messages and the request
  timeline (only when its spans were stored). Error-rate alerts name the top problems; log
  monitors include three matching lines.
- Looking this up gets at most 5 seconds (less near the end of an evaluation pass) and never
  blocks or fails the alert; `pyloggle_alert_enrich_failures_total` counts the times it gave up.
- Telegram: HTML by default with link previews off, fitted to Telegram's 4096-character limit by
  dropping detail rather than cutting text, and resent once as plain text if Telegram refuses
  the markup. Errors no longer include the bot token. `ALERT_TELEGRAM_API_URL` points at a
  self-hosted Bot API server.
- E-mail subjects are encoded for non-ASCII text and cannot carry line breaks; bodies are
  quoted-printable.
- The webhook payload and the stored firing gain `context` (title, summary, scope, window),
  `top_issues` and `samples`, with counts of people but never who they are, request URLs or
  the steps. *Alerts sent* in the UI shows the same summary sentence.

### Alerts only once, and a recap of yesterday

- Each thing alerts a rule once, recorded in a new `alert_notified` table (migration 016):
  - a new problem alerts once;
  - a problem that came back alerts once per comeback;
  - a problem that jumps alerts at most once a day;
  - too-many-errors and message-search rules alert when an incident starts, stay quiet while it
    lasts, and alert again only after a check finds things back to normal.

  "Wait before alerting again" still sets the shortest gap between two messages from a rule.
  Nothing is recorded when every notifier failed, so the alert is retried.
- **Fixed:** a "came back" rule alerted again every time someone assigned, commented on or
  snoozed a problem that had come back, because it read every saved change. It now reads the
  comebacks themselves from the activity log.
- **New rule kind, "a recap of yesterday, every day":**
  - Sent at a chosen hour in a chosen time zone, by e-mail, Slack, Telegram or webhook.
  - Covers errors compared with the day before, new problems, problems that came back, the ones
    that happened most, apps that need a look (failing, struggling or silent), incidents still
    going on, and the alerts sent.
  - It goes out once per local day, also after a restart, and on quiet days too.
  - *Test this alert* shows the recap without sending it.
  - Webhooks get it as `kind: "daily_recap"` with a `recap` object.
- **Upgrade note:** problems already inside a rule's window when you upgrade may alert one last
  time.

### Fixed

- The alert dialog's Slack field said "Empty uses the server's default", but an empty field
  turned Slack off, so the default webhook (`ALERT_SLACK_WEBHOOK_URL`) could not be chosen.
  When the server has one, the dialog now offers it as a checkbox, and rule cards that use it
  say "Slack".
- Sign-in allow-lists trusted an e-mail address the sign-in provider marked as unverified
  (`email_verified: false`), so on a provider where people can register any address, someone
  could pass `PYLOGGLE_OIDC_ALLOWED_DOMAINS` with an address that is not theirs. Unverified
  addresses, and user names in their place, no longer count. The refusal page now says why.

- The Requests search showed the logs search help.
- The filter buttons in a request's log panel did nothing.
- The top-issues sparkline on Dashboards was always empty.
- Issue detail kept loading forever when an issue had no stored occurrence.
- Company sign-in dropped the `?err=` message; an expired company session showed the
  access-key form.
- Choosing a project could spin forever when the selection failed.
- A signed-out browser could refetch `/me` in a loop.
- With ClickHouse down, the UI reloaded in a loop or offered to "create your first project".
  The API now answers `503 unavailable`, the UI says the database isn't answering, and it
  recovers on its own when the database is back.
- Several views showed an error and "nothing here" at the same time; dashboard cards said
  "No errors" when the request had failed.
- "Most frequent problems" on Dashboards, and the recap's top problems, counted people affected
  over the issue's whole life instead of the selected range, so a problem could show more
  people than times it happened. The recap's new and came-back problems are now counted for
  yesterday too, like the rest of the recap.
- The recap gave "last heard from" and "since" times in UTC while its day was in the rule's
  time zone. They now use the rule's time zone too.
- With `INGEST_SHARED_SECRET` set and `PYLOGGLE_DEFAULT_PROJECT_ID` empty, data sent with the
  shared secret was accepted without a project. It is now refused.
- The first database migration created a database named `pyloggle` whatever `CLICKHOUSE_DSN`
  named, so a ClickHouse user allowed only its own database could not start the server, and
  everyone else got a stray empty `pyloggle` database. Migrations now only touch the DSN's
  database.

## 0.3.0

Issue workflow, log-query monitors, traces, OpenTelemetry metrics, analytics and export,
storage accounting with per-project retention and deletion, Prometheus metrics and a System
page, generic OIDC login, scoped API keys, Django and Celery in the SDK. Migrations are
additive; see `docs/self-hosting/upgrading-0.3.md`.

### Issues

- Workflow: assignee (with suggestions and "assign to me"), comments, an activity timeline
  (status changes, assignments, comments, regressions), snooze until a time or until N more
  occurrences, resolve in the next or a named release (a newer release reopens it; `1.10`
  sorts after `1.9`, unknown releases fall back to first-seen order), bulk actions with the
  same options, an "assigned to me" filter.
- State changes are read-modify-write: a regression written by ingest no longer clears the
  assignee (0.2 took the latest value per column and lost it).

### Alerting

- `log_query` monitors: any explorer query compared (`gt`, `gte`, `lt`, `lte`, `eq`) with a
  threshold over a window; `lt 1` is an absence check. Evaluation runs `ALERTING_EVAL_DELAY`
  (30 s) behind real time so ingest lag does not false-fire. A "Test" button previews the count.
- Slack incoming-webhook notifier (per rule, `ALERT_SLACK_WEBHOOK_URL` as the default).
- Every alert carries a link into the UI and the matched count. Webhook payloads and stored
  firings carry a rule summary instead of the rule with its notifier addresses.

### Explorer

- Analytics view: count, avg, sum, min, max, p50, p95, p99 of `duration_ms`, `http.status`
  or any attribute, grouped by any field, as a multi-line chart (top 8 + other) and a table;
  saved views keep it.
- Export as CSV or NDJSON, streamed in pages up to `PYLOGGLE_EXPORT_MAX_ROWS` (100 000),
  with a formula-injection guard on CSV cells. Also on `/v1/q/logs/export`.
- Request stats (`request_stats_minutely`): requests, errors and latency digests per minute,
  method and route, feeding the Dashboards request tiles (req/s, 5xx %, p95, latency by route).

### Traces

- OTLP spans are stored (every kind, attributes, events; 7 days) and the Traces page lists
  root and server spans with a latency histogram and p50/p95 by name; a trace opens as an
  HTML waterfall with a span panel, the request's log lines and the issues it caused.
  Links from log lines and occurrences to their trace.

### Metrics

- OTLP metrics are stored (gauges, sums, histograms; 30 days) and the Dashboards get a metric
  card (avg, sum, min, max, last, rate, p50/p95/p99 for histograms, grouped by an attribute,
  top 10 + other). Cumulative series are differenced per series with reset detection and a
  pre-window baseline.
- The request tiles can read the OpenTelemetry HTTP semantic-convention metrics
  (`http.server.request.duration`, `http.server.active_requests`) instead of the logs.

### Storage

- Storage accounting per project, source and day (`project_usage_daily` through materialized
  views, calibrated with the real compression ratio), shown on the Projects page.
- `max_storage_gb` now caps the estimated bytes stored over the retention window: info and
  debug lines are dropped above it, errors keep flowing up to twice the quota.
- Per-project `retention_days` (shorter than the server's) trims older rows nightly with
  partition-scoped mutations.
- Project deletion (tombstone at once, purge in the background) and per-user purge, both
  audited; `GET /admin/audit`.
- Retention knobs for spans and metrics (`PYLOGGLE_RETENTION_SPANS_DAYS=7`,
  `PYLOGGLE_RETENTION_METRICS_DAYS=30`).

### Operations

- `GET /metrics` in Prometheus text format: ingest counters by project, door and outcome,
  queue depths and capacities, rows written and dropped, flush latencies, stream gauges,
  alerting counters, ClickHouse table sizes, parts and oldest partitions, disk, pending
  mutations, `pyloggle_build_info`. Optional `PYLOGGLE_METRICS_TOKEN`.
- System page (admins): version, uptime, ClickHouse status and tables, ingest rate and drops,
  queues, retention, alerting status.

### Auth and keys

- `PYLOGGLE_UI_AUTH=oidc`: any OpenID Connect provider (discovery, PKCE, RS256 and ES256,
  e-mail from `email`, `preferred_username` or `upn`), with domain and e-mail allow-lists.
  Azure is a preset of the same code and behaves as before.
- Scoped API keys: any number per project, each with a name and a subset of `ingest`, `read`
  and `write`; ingest doors need `ingest`, reads `read`, mutations `write`; revocation takes
  effect on the next request; last use is recorded. 0.2 keys are migrated as `default` keys.

### OpenTelemetry

- Three doors (`/v1/logs`, `/v1/traces`, `/v1/metrics`), a Collector example, a guide
  (`docs/send-data/opentelemetry.md`) and support for FastAPI 0.142+ native telemetry: spans with
  operation spans, HTTP metrics for the dashboards, exception logs as issues.
- One issue per crash: the first error occurrence per project, trace and exception class is
  the occurrence, a second report through another door within two minutes is a linked log
  line; spans stamped by the SDK produce no error of their own; failed child spans caused by
  an exception produce nothing; `OTLP_STORE_SERVER_SPANS` defaults to `true`; `url.path` is
  never used as a route.

### Python SDK 0.3

- `pyloggle.django`: middleware (sync and async), `settings.PYLOGGLE`, route template, user
  hash, exceptions captured once (Django's own error line is a duplicate), SQL breadcrumbs.
- `pyloggle.celery`: task scope with tags, failures captured once, retries as warnings,
  request ids carried from the publisher in message headers, optional task events, logging
  handler re-installed after the worker's logging setup.
- Fork safety: the sender thread restarts itself in forked children.
- Outgoing HTTP breadcrumbs for `requests`, `httpx` and `urllib3` (on by default).
- OpenTelemetry interop: events take the active span's ids, captured exceptions stamp the
  span, `pyloggle.fastapi.telemetry()` for FastAPI's native telemetry,
  `otel_owns_exceptions`. Request and error events carry `duration_ms`.

### Fixed

- Regression detection on an unknown release treated the first sighting as older.
- `SetIssueState` wrote rows that ClickHouse could order before the previous one when two
  changes landed in the same second.
- Cumulative OTLP series lost their first sample in every window (short-lived processes and
  windows shorter than the export interval counted nothing).

## 0.2.0

A redesign of the server, the UI and the Python SDK. Projects already sending data need no
change; see `docs/self-hosting/upgrading-0.2.md`.

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
  (`docs/self-hosting/retention-and-footprint.md`).

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
