# Upgrading to 0.3

0.3 adds issue workflow, log-query monitors, traces, OTLP metrics, analytics and export,
storage accounting with per-project retention and deletion, Prometheus metrics, generic OIDC,
scoped API keys and Django/Celery SDK integrations. Existing projects, SDKs and keys keep
working; this page lists what the first start does and what changes shape.

## Before you start

- **Back up ClickHouse** if the data matters. The migrations are additive (no table is
  rebuilt), but a backup makes a rollback trivial.
- Spans and metrics are now stored by default, 7 and 30 days. If your apps already export
  OpenTelemetry traces or metrics, expect new disk use (`docs/footprint.md` has numbers), or
  set `OTLP_STORE_SPANS=false` / `OTLP_STORE_METRICS=false`.
- Webhook consumers: the alert payload changes shape (below).

## What the first start does

Migrations `009` to `015`, each recorded in `schema_migrations`:

1. `009_alerting_v2`: `alert_rules` gains `query`, `comparison`, `slack_webhook_urls`;
   `alert_firings` gains `count` and `url`.
2. `010_issues_workflow`: `issues_state` gains `snooze_until`, `snooze_count`,
   `resolved_in_release`; new `issues_activity` (assignments, comments, status changes,
   regressions; two years) and `releases` (first time each release was seen), backfilled.
3. `011_request_stats`: `request_stats_minutely` (requests, errors and latency digests per
   minute, service, env, method and route) fed from `logs_raw`, backfilled from the retained
   window.
4. `012_spans`: the `spans` table (daily partitions, 7-day TTL).
5. `013_metrics`: `metrics_points` (30-day TTL) and the `metrics_catalog` view.
6. `014_usage`: `project_usage_daily` fed by one materialized view per event table, backfilled;
   `projects` gains `retention_days` and `is_deleted`; new `admin_audit`.
7. `015_api_keys`: `project_api_keys` and `project_api_key_usage`; every live project's key is
   copied as its `default` key with the scopes `ingest`, `read`, `write`.

Backfills are guarded, so a restart never doubles them. Retention for the new tables is applied
like the old ones (`MODIFY TTL` when the configured days change).

## What changes shape

- **Alert webhooks and firings.** The payload's `rule` is now a summary (`id`, `name`,
  `project_id`, `service`, `env`, `http_route`, `http_method`, `condition_type`, `threshold`,
  `time_window_seconds`, `query`, `comparison`); the notifier addresses (e-mail, webhook and
  Slack URLs, chat ids) are no longer sent to receivers or stored with firings. Every alert
  carries a `url` into the UI when `PYLOGGLE_PUBLIC_URL` is set, and a `count`.
- **API keys.** Keys live in `project_api_keys` with scopes. The 0.2 key of each project
  becomes its `default` key with every scope, so nothing to change in your apps. New keys are
  `pl_` + 43 characters; the Projects page creates them with a name and a subset of `ingest`,
  `read`, `write`, lists their prefix and last use, and revokes them (effective on the next
  request). "Rotate key" is gone from the UI; `POST /projects/{id}/rotate-key` and the 0.1
  admin endpoint still work and now replace the key named `default`. A key without the needed
  scope gets `403 insufficient_scope`: ingest doors need `ingest`, reads need `read`,
  mutations and the UI's own changes need `write`. Logging into the UI with a key needs
  `read`. `projects.api_key_hash` is no longer read or written (dropped in a later release).
- **Login.** `PYLOGGLE_UI_AUTH=azure` behaves exactly as before. `PYLOGGLE_UI_AUTH=oidc` with
  `PYLOGGLE_OIDC_*` works with any OpenID Connect provider (discovery, PKCE, RS256/ES256).
  `PYLOGGLE_OIDC_ALLOWED_DOMAINS` / `_ALLOWED_EMAILS` restrict both modes.
- **Storage quota semantics.** `max_storage_gb` was a per-day cap on raw bytes kept in memory
  and reset on restart. It now means *estimated bytes stored over the retention window*
  (from `project_usage_daily`, calibrated with the real compression ratio): over the quota,
  info and debug lines are dropped while errors keep flowing up to twice the quota. The
  Projects page shows the usage bar and the per-source breakdown.
- **Per-project retention.** `retention_days` on a project (only shorter than the server
  retention) removes that project's older rows from `logs_raw`, `error_occurrences` and
  `spans` with one partition-scoped mutation per table and day, nightly at 03:10 UTC and
  shortly after start. Mutations run one per table at a time.
- **Deletion.** `DELETE /projects/{id}` tombstones the project at once (keys stop resolving,
  nothing lists it) and purges every table in the background; `POST
  /projects/{id}/purge-user` removes one user's log lines and occurrences (`issues_agg`
  affected-user counts cannot shrink). Both are audited (`GET /admin/audit`).
- **OTLP.** `OTLP_STORE_SERVER_SPANS` now defaults to `true` (server spans become `request`
  lines that feed the explorer, analytics and request stats). `url.path` / `http.target` are
  no longer used as a route. Failed spans: see `docs/opentelemetry.md` for which failures
  become issues. The server folds two reports of the same crash on one trace into one
  occurrence.
- **Request latency.** Request lines carry `duration_ms` as a column; the 0.2 SDKs'
  `attrs.latency_ms` is promoted into it on ingest. Analytics (`p95` of `duration_ms` by
  route) and the Dashboards request tiles read the column.
- **Prometheus.** `GET /metrics` on the ingest port exposes counters, queue gauges, flush
  histograms and ClickHouse table sizes; set `PYLOGGLE_METRICS_TOKEN` to require a bearer
  token. `/healthz?verbose=1` is unchanged.
- **Admins.** The System page, project deletion, purges, key management and the audit log are
  limited to `PYLOGGLE_UI_ADMINS` when a login mode is on.

## Python SDK 0.3

`pip install -U pyloggle`. Nothing that existed changes signature or behaviour by default,
with two additions you get for free and can turn off:

- Outgoing `requests`, `httpx` and `urllib3` calls become `http.client` breadcrumbs
  (`http_breadcrumbs=False`).
- When `opentelemetry-api` is installed and a span is active, events take its trace and span
  ids (over the incoming `traceparent`'s), a captured exception stamps the span with
  `pyloggle.event_id`, and an active server span replaces the integrations' `request` line.

New: `pyloggle.django` (middleware, settings-driven init, user, SQL breadcrumbs),
`pyloggle.celery` (task scope, failures, retries, publish headers, fork safety),
`pyloggle.fastapi.telemetry()` for FastAPI's native telemetry, `otel_owns_exceptions`,
`db_breadcrumbs`, `duration_ms` on request and error events, extras `django`, `celery`, `otel`.
The sender thread restarts itself in forked children (Celery prefork, gunicorn `--preload`).

## Rolling back

Stop `ingestd`, deploy the 0.2 image and restore the backup, or keep the database: 0.2 ignores
the new tables and columns, reads `projects.api_key_hash` (which 0.3 left untouched for
existing projects; projects and keys created by 0.3 would not resolve) and does not know the
`log_query` alert condition (skip it before rolling back).
