# Upgrading to 0.2

0.2 is a redesign of the server, the UI and the Python SDK. Existing projects keep working with
no code change; this page lists what happens on the first start and what you may want to adjust.

## Before you start

- **Back up ClickHouse** if the data matters (`clickhouse-backup`, a volume snapshot, or
  `docker compose stop && cp -r` of the volume). The upgrade rebuilds two tables.
- Decide how much history you need. The default retention is **30 days** for logs and errors
  (`PYLOGGLE_RETENTION_LOGS_DAYS`, `PYLOGGLE_RETENTION_ERRORS_DAYS`). Rows older than that are not
  copied into the new tables.
- `docker compose` now mounts `deploy/clickhouse/config.d` and `users.d` into ClickHouse and sets
  memory limits. Review [Retention and footprint](retention-and-footprint.md) if your box is unusually small or large.

## What the first start does

1. `ingestd` connects (retrying for up to two minutes), then applies the embedded migrations
   and records them in `schema_migrations`. The old `docker-entrypoint-initdb.d` DDL is gone;
   nothing else needs to run SQL.
2. Migration 006 rebuilds `logs_raw` and `error_occurrences` with daily partitions,
   `project_id`-first sort keys, codecs and the new report columns. It copies **only the
   retention window**, day by day, logging progress, then swaps the tables atomically. Set
   `PYLOGGLE_MIGRATE_REBUILD=false` to add the columns in place instead (keeps monthly partitions;
   expiry then happens through merges).
3. Migration 007 replaces the broken `issues_stats` with `issues_agg`, `issue_counts_hourly` and
   `log_counts_minutely`, backfilled from the copied occurrences. Issue counts, affected users
   and first-seen times become correct for all retained history.
4. Retention is applied as `MODIFY TTL` whenever the configured days change, once.

Startup logs every step. On a 1 M-row day the copy takes seconds; plan a few minutes for a
large install. The server answers `/healthz` only after migrations finish.

## What changes for existing data

- **Issue identity changes once.** Fingerprint v2 groups by exception type, module and the
  in-app call chain (no line numbers), so an issue survives edits elsewhere in the file. Old
  issues keep their rows and history; new occurrences of the same bug may open a new issue.
  Resolve or ignore the old ones in bulk from the Issues page.
- Sampling defaults change: `INGEST_SAMPLE_INFO` is now `1.0` (was 0.2) and
  `INGEST_SAMPLE_DEBUG` `0.1` (was 0.05). A log explorer must show the lines people search for;
  volume is bounded by the TTL, codecs and quotas instead. Set them back if you relied on them.
- `POST /v1/logs` is now the OTLP/HTTP logs endpoint. `GET /v1/logs` is unchanged. The query API
  lives under `/v1/q/*` (same routes as the UI's `/ui/api/*`, authenticated with `X-API-Key`).

## Python SDK

`pip install -U pyloggle`. Nothing that existed changes signature:
`Client(ClientConfig(...))`, `install_pyloggle_logging`, `pyloggle.flask.init_app`,
`pyloggle.fastapi.PyloggleFastAPIMiddleware` and the env vars all work as before.

New behaviour you get for free:

- Tracebacks carry module, source context, in-app flags, local variables (in-app frames, secrets
  scrubbed), chained causes, exception groups and notes.
- `Client()` installs `sys.excepthook`, `threading.excepthook` and `sys.unraisablehook` so
  uncaught errors in scripts and threads become issues. Opt out with
  `install_excepthooks=False`.
- One error event per failing request: the framework's own error log line is kept but marked
  `skip_issue`, so it no longer creates a second issue.
- Timestamps are RFC 3339 with milliseconds and a zone; attribute values are stringified; both
  were silently rejected by the old server for stdlib-logging events.

New API (optional): `pyloggle.init()`, `capture_exception`, `capture_message`, `add_breadcrumb`,
`set_user`, `set_tag(s)`, `set_context`, `push_scope`, `capture_errors`, `flush`, `close`.

## OpenTelemetry

Point an OTLP/HTTP exporter at `http(s)://<host>/v1/logs` (and `/v1/traces`) with header
`X-API-Key: <project key>` or `Authorization: Bearer <key>`. Protobuf and JSON encodings are
accepted; Python tracebacks in `exception.stacktrace` are parsed back into structured frames.

## UI

The UI is a single-page app embedded in the binary. Bookmarks to `/ui/issues`, `/ui/logs`,
`/ui/dashboards` and `/ui/projects` keep working; every view keeps its state in the URL and
can be shared. `/ui/login` still starts the Azure flow when `PYLOGGLE_UI_AUTH=azure`.
`PYLOGGLE_UI_ADMINS` (comma-separated emails) optionally restricts project and alert changes.

## Rolling back

Stop `ingestd`, restore the ClickHouse backup, deploy the previous image. The 0.1 server does
not understand the v2 tables; do not point it at an upgraded database.
