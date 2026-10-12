# Pyloggle

Self-hosted **logs, error tracking, traces and metrics** for Python services. One Go binary
(`ingestd`) and one ClickHouse. Small enough for a single VM, with a Datadog-style log explorer,
Sentry-style issues and an OpenTelemetry door.

- **Catches tracebacks properly**: structured frames with source context and locals, chained
  causes, breadcrumbs, request/user/runtime context, uncaught errors in scripts and threads.
- **Groups them sensibly**: fingerprints follow the in-app call chain, not line numbers; resolved
  issues that come back are flagged as regressions; one crash reported through the SDK and
  OpenTelemetry is one issue.
- **Works them**: assign, comment, snooze until a time or N occurrences, resolve in the next
  release, an activity timeline; alerts on error rate, spikes, new issues, regressions or any
  log query, to e-mail, webhook, Slack and Telegram. An alert says what happened in plain words
  and, for the first few problems, where the code failed, the steps just before, how many
  people it hit, and links to that time, the request's messages and its timeline.
- **Logs you can actually search**: `service:api level:error "timed out" -attrs.path:/health`,
  facets with counts, a stacked timeline you can drag to zoom, patterns, live tail, saved views,
  analytics (p95 of `duration_ms` by route) and CSV/NDJSON export.
- **Traces and metrics**: OTLP spans in a waterfall linked to the request's logs and issues;
  OTLP metrics on the dashboards, including FastAPI's native HTTP metrics.
- **Consumes little, and you can see it**: daily partitions with TTLs that drop whole parts,
  tuned ClickHouse config, per-project usage, quotas, retention and deletion, Prometheus
  `/metrics` and a System page. Numbers in [docs/footprint.md](docs/footprint.md).
- **Two doors in**: the dependency-free Python SDK (Flask, FastAPI, Django, Celery; zero-change
  upgrade from 0.1) or any OpenTelemetry exporter with a scoped API key.

## Quick start

```bash
cd deploy && docker compose up --build -d      # ClickHouse + ingestd on :8080
open http://localhost:8080/ui/                 # UI auth is disabled in the compose file
```

Create a project under **Projects**; the API key is shown once. Then, in your app:

```bash
pip install pyloggle
export PYLOGGLE_INGEST_URL=http://localhost:8080
export PYLOGGLE_API_KEY=pl_…            # from the Projects page
export PYLOGGLE_PROJECT_ID=my-service
```

```python
import pyloggle
pyloggle.init(service="checkout", env="prod")      # logging handler + exception hooks + enrichment
```

Everything that goes through `logging` is now a searchable log line; every uncaught exception
and every `logging.exception(...)` is an issue with a full report. For web apps add the
framework integration:

```python
from pyloggle.flask import init_app;  init_app(app, client)                       # Flask
from pyloggle.fastapi import PyloggleFastAPIMiddleware                           # FastAPI / any ASGI
app.add_middleware(PyloggleFastAPIMiddleware, client=client)
MIDDLEWARE = ["pyloggle.django.PyloggleMiddleware", *MIDDLEWARE]                 # Django (settings.PYLOGGLE)
import pyloggle.celery; pyloggle.celery.init()                                   # Celery
```

The SDK's full API (capture, breadcrumbs, scopes, user, tags, configuration) is in
[python-sdk/README.md](python-sdk/README.md). Runnable examples: `examples/script.py`,
`examples/flask_app.py`, `examples/fastapi_app.py`, `examples/django_app.py`,
`examples/celery_app.py`, `examples/otel_app.py`.

### OpenTelemetry, with or without the SDK

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080        # /v1/logs, /v1/traces, /v1/metrics
export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=pl_…"
export FASTAPI_OTEL_AUTO_CONFIGURE=true                          # FastAPI 0.143+ native telemetry
```

Protobuf or JSON, gzip, logs, traces and metrics, from an SDK exporter or the Collector
(`examples/otel-collector.yaml`). FastAPI 0.142+ needs nothing else: its request and operation
spans fill the Traces page, its HTTP metrics the dashboards, its exception logs become issues
with parsed frames. With the SDK alongside (`pyloggle.fastapi.telemetry()`), the SDK keeps the
rich error reports and the trace view links both. Details in
[docs/opentelemetry.md](docs/opentelemetry.md).

## The UI

The UI is written for the whole team, not only engineers: every page says in one line what
it is for, labels use plain words with the technical name in a tooltip ("Slow (p95)",
"Came back (regressed)", "Request timeline (trace)"), and monospace is kept for code, ids and
queries. `/ui/` opens on **Overview**. The sidebar groups pages by what you are doing:

| Group | Page | What it answers |
|---|---|---|
| | **Overview** | *Is everything OK right now?* A status line ("All clear" or what needs attention, with links), errors, people affected, failed requests, response time and messages with the change against the previous period, issues that are new, came back or jumped, each app's health (OK, degraded, failing or silent), recent versions and recent alerts |
| Investigate | **Issues** | Errors grouped by cause, in Unresolved / Came back / Resolved / Ignored / All tabs. Filter by app, environment, version and assignee; sort by most recent, newest, most frequent or most people; 24 h trend per issue; resolve, ignore, snooze or reopen several at once |
| | **Issue** | What went wrong and where (your code first, all code, or plain text), the steps before the error, the web request, user and system, labels, how often it happened, which apps, versions, servers and routes it happens on, and every recent time it happened. Assign, comment, resolve now or in the next version, ignore until it happens N more times; an activity timeline in plain sentences |
| | **Logs** | Every message your apps wrote. Search with plain chips ("Severity is Error", "App is api") over the [query language](docs/log-query-language.md), or the *Add filter* picker; filters sidebar with counts; a chart colored by severity, app, environment and more (drag to zoom, "each bar is 30 minutes"); *Similar messages* and *Charts* views; message details with "all messages from this request", the request timeline and surrounding messages; live mode, saved searches, CSV / NDJSON export |
| | **Requests** | Each request your apps handled, step by step (traces): how long requests take, the busiest operations with typical / slow / failed share, and a timeline per request with step details, the messages logged during it and the errors it caused |
| Monitor | **Dashboards** | How the project is doing over time, in three sections: errors, traffic (requests, failed share, response time, slowest endpoints, from app logs or OpenTelemetry) and activity, plus custom measurements from any OpenTelemetry metric |
| | **Alerts** | Rules written as sentences ("Alert me when a problem suddenly jumps by more than 3 times compared with the previous 5 minutes, for api in prod, by e-mail and Slack"), a *Test this alert* button, and the alerts sent recently in plain words |
| Settings | **Projects** | Projects with their limits and storage, a *Connect your app* guide (Python, Django, FastAPI, OpenTelemetry) with this server's address filled in, access keys (Send data / View / Make changes), storage per kind of data, deletion, and admin activity |
| | **System** | Health of the Pyloggle server itself, for administrators: database, disk, incoming and dropped data, queues, cleanup tasks, the alerts engine and storage per table |

Keyboard: `/` focuses search, `j` / `k` move through rows, `Esc` closes a panel, `g` then
`o` / `i` / `l` / `t` / `d` / `a` / `p` / `s` jumps between pages, and `?` lists every shortcut.
Light and dark themes follow the OS or the toggle. Below 1200 px wide the sidebar shrinks to
icons. Every view keeps its state in the URL, so it can be shared. If ClickHouse stops
answering, pages say so and come back on their own when it returns.

The query language is documented in [docs/log-query-language.md](docs/log-query-language.md).

## HTTP API

| Route | Auth | Purpose |
|---|---|---|
| `POST /v1/ingest` | API key | NDJSON events ([wire format](docs/wire-format-v2.md)); answers `{accepted, rejected, errors, dropped}` |
| `POST /v1/logs`, `/v1/traces`, `/v1/metrics` | API key | OTLP/HTTP (also under `/v1/otlp/v1/*`) |
| `GET /v1/q/…` | API key (`X-API-Key` or `Bearer`) | the same JSON API the UI uses: `issues`, `issues/{id}`, `issues/{id}/trend`, `issues/{id}/breakdown`, `issues/{id}/occurrences`, `issues/{id}/activity`, `events/{id}`, `logs`, `logs/histogram`, `logs/facets`, `logs/patterns`, `logs/context`, `logs/aggregate`, `logs/export`, `traces`, `traces/{trace_id}`, `metrics/overview`, `metrics/errors`, `metrics/logs`, `metrics/top-issues`, `metrics/requests`, `metrics/services`, `metrics/attention`, `metrics/names`, `metrics/series`, `releases`, `saved-views`, `projects`, `projects/{id}/usage`, `projects/{id}/keys`, `alerts/rules`, `alerts/firings`, `admin/audit`, `system`; `POST` to change issue state, assign, comment, save views, manage projects, keys and rules. Keys need the `read` scope for GETs and `write` for the rest |
| `GET /ui/api/…` | browser session | identical routes for the SPA (mutations need the `X-Pyloggle-Request: 1` header) |
| `GET /metrics` | optional bearer (`PYLOGGLE_METRICS_TOKEN`) | Prometheus exposition: ingest, queues, alerting, ClickHouse tables, build info |
| `GET /v1/issues`, `/v1/logs`, `/v1/issues/occurrences`, `/v1/saved-searches`, `/v1/metrics/errors/trend` | API key | 0.1 query endpoints, kept for existing scripts |
| `POST /v1/issues/state`, `/v1/alerts/rules`, `/v1/projects*` | `X-Admin-Token` | 0.1 admin endpoints, kept |
| `GET /healthz`, `/healthz?verbose=1` | none | liveness; verbose adds queue depth and drop counters |

`metrics/overview` and `metrics/requests` take `compare=previous` to add the same-length period
before the range. When ClickHouse is unreachable the JSON API answers `503 unavailable`.

Without an `X-API-Key`, requests fall back to `PYLOGGLE_DEFAULT_PROJECT_ID` (reads only on
`/v1/q`). Set it to an empty string to require keys everywhere. Ingest doors need a key with
the `ingest` scope; a key without the needed scope answers `403 insufficient_scope`.

## Configuration

All configuration is environment variables on `ingestd`.

| Variable | Default | Meaning |
|---|---|---|
| `CLICKHOUSE_DSN` | `clickhouse://localhost:9000/pyloggle` | native protocol DSN |
| `INGEST_PORT` | `8080` | listen port |
| `PYLOGGLE_DEFAULT_PROJECT_ID` | `default` | project for keyless requests (auto-created) |
| `PYLOGGLE_DEFAULT_SERVICE` / `PYLOGGLE_DEFAULT_ENV` | `unknown` / `prod` | fallbacks for events without them |
| `PYLOGGLE_PUBLIC_URL` | | external URL used in alert links and the Projects page |
| `PYLOGGLE_AUTO_MIGRATE` | `true` | apply embedded migrations at startup |
| `PYLOGGLE_MIGRATE_REBUILD` | `true` | 0.1 → 0.2 upgrade rebuilds tables (copies the retention window); `false` alters in place |
| `PYLOGGLE_RETENTION_LOGS_DAYS` / `_ERRORS_DAYS` / `_STATS_DAYS` / `_SPANS_DAYS` / `_METRICS_DAYS` | `30` / `30` / `365` / `7` / `30` | TTLs, applied as `MODIFY TTL` when changed |
| `INGEST_SAMPLE_INFO` / `_DEBUG` / `_WARN` | `1.0` / `0.1` / `1.0` | keep ratios by level; errors are always kept |
| `INGEST_SAMPLE_BY_REQUEST` | `true` | sampling decisions are per request id, so a kept request keeps all its lines |
| `INGEST_RATE_LIMIT_RPS` / `INGEST_RATE_LIMIT_BURST` | `2000` / `4000` | per-service limiter (projects can set `max_events_per_second`) |
| `INGEST_MAX_QUEUE_SIZE` / `INGEST_BATCH_SIZE` / `INGEST_BATCH_TIMEOUT` | `10000` / `500` / `5s` | write path |
| `OTLP_STORE_SPANS` / `OTLP_STORE_METRICS` | `true` / `true` | keep OTLP spans and metrics (issues and request lines derived from spans are kept either way) |
| `OTLP_STORE_SERVER_SPANS` | `true` | server spans also produce `request` log lines (explorer, analytics, request stats) |
| `PYLOGGLE_EXPORT_MAX_ROWS` | `100000` | hard cap for CSV / NDJSON exports |
| `PYLOGGLE_METRICS_TOKEN` | | bearer token required on `GET /metrics` when set |
| `PYLOGGLE_UI_AUTH` | `azure` | `disabled` for local use, `azure` for Entra ID single-tenant login, `oidc` for any OpenID Connect provider |
| `PYLOGGLE_AZURE_TENANT_ID`, `_CLIENT_ID`, `_CLIENT_SECRET`, `PYLOGGLE_AZURE_REDIRECT_URL` | | `azure` preset (fixed v2.0 endpoints, tenant check, object id as subject) |
| `PYLOGGLE_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET`, `_SCOPES`, `_REDIRECT_URL`, `_DISPLAY_NAME` | `openid profile email` scopes | `oidc` mode: discovery at `<issuer>/.well-known/openid-configuration`, RS256 and ES256 id_tokens; the display name labels the login button |
| `PYLOGGLE_OIDC_ALLOWED_DOMAINS` / `_ALLOWED_EMAILS` | | optional allow-lists for both login modes (anyone else gets a 403 after signing in) |
| `PYLOGGLE_UI_SESSION_SECRET` | | required by both login modes; ≥ 32 chars or base64 of ≥ 16 bytes |
| `PYLOGGLE_UI_ADMINS` | | comma-separated emails allowed to change projects and alert rules (login modes) |
| `PYLOGGLE_ADMIN_TOKEN` | | enables the 0.1 admin endpoints |
| `ALERTING_ENABLED` / `ALERTING_POLL_INTERVAL` / `ALERTING_EVAL_DELAY` | `false` / `30s` / `30s` | rule evaluator; the delay keeps log-query monitors behind ingest lag |
| `ALERT_SLACK_WEBHOOK_URL` | | default Slack incoming webhook (rules can carry their own) |
| `ALERT_SMTP_HOST`, `_PORT`, `_USERNAME`, `_PASSWORD`, `_FROM`, `_TLS` | | email notifier |
| `ALERT_WEBHOOK_TIMEOUT`, `ALERT_TELEGRAM_BOT_TOKEN`, `ALERT_TELEGRAM_TIMEOUT` | | webhook / Telegram notifiers |
| `ALERT_TELEGRAM_PARSE_MODE` | `HTML` | Telegram messages are HTML (links, bold, code); `none` sends plain text. A message Telegram refuses to parse is resent once as plain text |
| `ALERT_TELEGRAM_API_URL` | `https://api.telegram.org` | Bot API address, for a self-hosted Bot API server |
| `GOMEMLIMIT` | `200MiB` (compose) | Go soft memory limit |

ClickHouse's own tuning lives in `deploy/clickhouse/{config.d,users.d}/pyloggle.xml` and is
explained in [docs/footprint.md](docs/footprint.md).

## Retention and footprint

Logs, error occurrences, spans and metric points live in daily partitions with a TTL (30, 30, 7
and 30 days by default). Expiry drops whole parts, so it costs no merge CPU. Aggregates the UI
reads (issue counts, per-level volume, request stats) are tiny and kept for a year. Per project:
`max_storage_gb` caps the estimated bytes stored over the window (errors keep flowing up to
twice the cap), `retention_days` trims older rows nightly, and the Projects page shows usage by
source. Measure your own install with `make footprint`.

## Upgrading

From 0.2: `docker compose pull && docker compose up -d` (or deploy the new binary). The first
start applies additive migrations (workflow, request stats, spans, metrics, usage, keys) and
copies each project's key as its `default` scoped key; `pip install -U pyloggle` needs no code
change. Read [docs/upgrading-0.3.md](docs/upgrading-0.3.md): webhook payloads, quota semantics
and the `OTLP_STORE_SERVER_SPANS` default changed. From 0.1, read
[docs/upgrading-0.2.md](docs/upgrading-0.2.md) first.

## Development

```bash
make build        # web UI (npm ci + vite) then the Go binary with the UI embedded
make test         # go vet + go test -race, pytest, vitest + typecheck
make dev-api      # ingestd against the compose ClickHouse, UI auth disabled
make dev-web      # Vite dev server on :5173 proxying /ui/api and /v1 to :8080
make screenshots  # Playwright screenshots of every page, light and dark (WIDTH=1024 for narrow)
make footprint    # RSS and disk measurement under a synthetic burst
```

Integration tests need a ClickHouse: `PYLOGGLE_TEST_CLICKHOUSE_DSN=clickhouse://default:@127.0.0.1:9000/default go test -tags integration ./internal/storage/clickhouse/`.
They cover a fresh install, the 0.1 → 0.3 upgrade (rebuild and in-place, key backfill),
retention, the whole ingest-to-query path, saved views, issue workflow, analytics and request
stats, spans, metrics, usage, trimming, deletion and API keys. The SDK tests need
`pip install -e "python-sdk[test]"` (Flask, FastAPI 0.143, Django, Celery, requests, the
OpenTelemetry SDK).

Layout: `cmd/ingestd` (main), `internal/ingest` (pipeline, quota, dedupe), `internal/issue`
(fingerprints, regression, releases), `internal/alerting`, `internal/logquery` (search syntax),
`internal/otlp` (logs, traces, metrics mapping), `internal/metrics` (Prometheus registry),
`internal/sysinfo`, `internal/tenant` (keys and scopes), `internal/storage/clickhouse` (client,
migrations, queries), `internal/webui` (JSON API, OIDC, SPA host), `web/` (React app),
`python-sdk/`, `deploy/`, `docs/`, `examples/`.

## License

See `LICENSE`.
