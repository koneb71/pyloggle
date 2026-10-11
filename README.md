# Pyloggle

Self-hosted **logs + error tracking** for Python services. One Go binary (`ingestd`) and one
ClickHouse. Small enough for a single VM, with a Datadog-style log explorer and Sentry-style
issues.

- **Catches tracebacks properly**: structured frames with source context and locals, chained
  causes, breadcrumbs, request/user/runtime context, uncaught errors in scripts and threads.
- **Groups them sensibly**: fingerprints follow the in-app call chain, not line numbers; resolved
  issues that come back are flagged as regressions.
- **Logs you can actually search**: `service:api level:error "timed out" -attrs.path:/health`,
  facets with counts, a stacked timeline you can drag to zoom, patterns, live tail, saved views.
- **Consumes little**: daily partitions with a 30-day TTL that drops whole parts, tuned
  ClickHouse config, ~140 MB of anonymous memory for ClickHouse and ~15 MB for ingestd at idle.
  Numbers in [docs/footprint.md](docs/footprint.md).
- **Two doors in**: the dependency-free Python SDK (zero-change upgrade from 0.1) or any
  OpenTelemetry exporter pointed at `/v1/logs` with the same API key.

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
```

The SDK's full API (capture, breadcrumbs, scopes, user, tags, configuration) is in
[python-sdk/README.md](python-sdk/README.md). Runnable examples: `examples/script.py`,
`examples/flask_app.py`, `examples/fastapi_app.py`, `examples/otel_app.py`.

### OpenTelemetry instead of the SDK

```python
exporter = OTLPLogExporter(endpoint="http://localhost:8080/v1/logs", headers={"X-API-Key": KEY})
```

Protobuf or JSON, gzip, logs and traces (`/v1/traces`). Python tracebacks in
`exception.stacktrace` are parsed back into frames. Details in
[docs/wire-format-v2.md](docs/wire-format-v2.md#otlp).

## The UI

| Page | What it does |
|---|---|
| **Issues** | open / regressed / resolved / ignored tabs with counts, search, sort by last seen, first seen, events or users, 24 h sparklines, bulk resolve / ignore / reopen |
| **Issue** | occurrence trend, exception chain with the crash site first (in-app, all frames or raw), source context and locals, breadcrumbs, request, user and runtime, tags and attributes, the request's log lines, raw JSON, older / newer navigation, breakdowns by env, release, route, host and user |
| **Logs** | query bar with chips, inline errors and autocomplete; facet panel (level, service, env, logger, host, release, HTTP method / status / route, any attribute or tag key); stacked timeline grouped by level, service, env, logger or status with drag-to-zoom; patterns view; virtualized list with a column picker; detail panel where every value offers *filter / exclude / copy*; same-request and same-trace links; view in context; live tail; saved views |
| **Dashboards** | events, errors, open / new issues, affected users and services for the range; error trend; log volume by level; top issues |
| **Alerts** | rules (error rate, issue spike, new issue, regression) with email, webhook and Telegram targets; recent firings |
| **Projects** | create, edit, rotate keys, switch the active project |

Keyboard: `/` focuses search, `j` / `k` move through rows, `Esc` closes the panel, `g i` / `g l` /
`g d` / `g a` / `g p` jump between pages. Light and dark themes follow the OS or the toggle.
Every view keeps its state in the URL, so it can be shared.

The query language is documented in [docs/log-query-language.md](docs/log-query-language.md).

## HTTP API

| Route | Auth | Purpose |
|---|---|---|
| `POST /v1/ingest` | API key | NDJSON events ([wire format](docs/wire-format-v2.md)); answers `{accepted, rejected, errors, dropped}` |
| `POST /v1/logs`, `/v1/traces`, `/v1/metrics` | API key | OTLP/HTTP (also under `/v1/otlp/v1/*`) |
| `GET /v1/q/…` | API key (`X-API-Key` or `Bearer`) | the same JSON API the UI uses: `issues`, `issues/{id}`, `issues/{id}/trend`, `issues/{id}/occurrences`, `events/{id}`, `logs`, `logs/histogram`, `logs/facets`, `logs/patterns`, `logs/context`, `metrics/overview`, `metrics/errors`, `metrics/logs`, `metrics/top-issues`, `saved-views`, `projects`, `alerts/rules`, `alerts/firings`; `POST` to change issue state, save views, manage projects and rules |
| `GET /ui/api/…` | browser session | identical routes for the SPA (mutations need the `X-Pyloggle-Request: 1` header) |
| `GET /v1/issues`, `/v1/logs`, `/v1/issues/occurrences`, `/v1/saved-searches`, `/v1/metrics/errors/trend` | API key | 0.1 query endpoints, kept for existing scripts |
| `POST /v1/issues/state`, `/v1/alerts/rules`, `/v1/projects*` | `X-Admin-Token` | 0.1 admin endpoints, kept |
| `GET /healthz`, `/healthz?verbose=1` | none | liveness; verbose adds queue depth and drop counters |

Without an `X-API-Key`, requests fall back to `PYLOGGLE_DEFAULT_PROJECT_ID` (reads only on
`/v1/q`). Set it to an empty string to require keys everywhere.

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
| `PYLOGGLE_RETENTION_LOGS_DAYS` / `_ERRORS_DAYS` / `_STATS_DAYS` | `30` / `30` / `365` | TTLs, applied as `MODIFY TTL` when changed |
| `INGEST_SAMPLE_INFO` / `_DEBUG` / `_WARN` | `1.0` / `0.1` / `1.0` | keep ratios by level; errors are always kept |
| `INGEST_SAMPLE_BY_REQUEST` | `true` | sampling decisions are per request id, so a kept request keeps all its lines |
| `INGEST_RATE_LIMIT_RPS` / `INGEST_RATE_LIMIT_BURST` | `2000` / `4000` | per-service limiter (projects can set `max_events_per_second`) |
| `INGEST_MAX_QUEUE_SIZE` / `INGEST_BATCH_SIZE` / `INGEST_BATCH_TIMEOUT` | `10000` / `500` / `5s` | write path |
| `OTLP_STORE_SERVER_SPANS` | `false` | store OK server spans as `request` logs |
| `PYLOGGLE_UI_AUTH` | `azure` | `disabled` for local use, `azure` for Entra ID single-tenant login, `oidc` for any OpenID Connect provider |
| `PYLOGGLE_AZURE_TENANT_ID`, `_CLIENT_ID`, `_CLIENT_SECRET`, `PYLOGGLE_AZURE_REDIRECT_URL` | | `azure` preset (fixed v2.0 endpoints, tenant check, object id as subject) |
| `PYLOGGLE_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET`, `_SCOPES`, `_REDIRECT_URL`, `_DISPLAY_NAME` | `openid profile email` scopes | `oidc` mode: discovery at `<issuer>/.well-known/openid-configuration`, RS256 and ES256 id_tokens; the display name labels the login button |
| `PYLOGGLE_OIDC_ALLOWED_DOMAINS` / `_ALLOWED_EMAILS` | | optional allow-lists for both login modes (anyone else gets a 403 after signing in) |
| `PYLOGGLE_UI_SESSION_SECRET` | | required by both login modes; ≥ 32 chars or base64 of ≥ 16 bytes |
| `PYLOGGLE_UI_ADMINS` | | comma-separated emails allowed to change projects and alert rules (login modes) |
| `PYLOGGLE_ADMIN_TOKEN` | | enables the 0.1 admin endpoints |
| `ALERTING_ENABLED` / `ALERTING_POLL_INTERVAL` | `false` / `30s` | rule evaluator |
| `ALERT_SMTP_HOST`, `_PORT`, `_USERNAME`, `_PASSWORD`, `_FROM`, `_TLS` | | email notifier |
| `ALERT_WEBHOOK_TIMEOUT`, `ALERT_TELEGRAM_BOT_TOKEN`, `ALERT_TELEGRAM_TIMEOUT`, `ALERT_TELEGRAM_PARSE_MODE` | | webhook / Telegram notifiers |
| `GOMEMLIMIT` | `200MiB` (compose) | Go soft memory limit |

ClickHouse's own tuning lives in `deploy/clickhouse/{config.d,users.d}/pyloggle.xml` and is
explained in [docs/footprint.md](docs/footprint.md).

## Retention and footprint

Logs and error occurrences live in daily partitions with a TTL (30 days by default). Expiry drops
whole parts, so it costs no merge CPU. Aggregates the UI reads (issue counts, per-level volume)
are tiny and kept for a year. Per-project `max_storage_gb` caps a tenant. Measure your own
install with `make footprint`.

## Upgrading from 0.1

`docker compose pull && docker compose up -d` (or deploy the new binary). The first start
migrates the schema, rebuilding the two big tables with only the retention window, and corrects
issue statistics. The SDK upgrade is `pip install -U pyloggle` with no code change. Read
[docs/upgrading-0.2.md](docs/upgrading-0.2.md) first: issue identity changes once and sampling
defaults changed.

## Development

```bash
make build        # web UI (npm ci + vite) then the Go binary with the UI embedded
make test         # go vet + go test -race, pytest, vitest + typecheck
make dev-api      # ingestd against the compose ClickHouse, UI auth disabled
make dev-web      # Vite dev server on :5173 proxying /ui/api and /v1 to :8080
make screenshots  # Playwright screenshots of every page, light and dark
make footprint    # RSS and disk measurement under a synthetic burst
```

Integration tests need a ClickHouse: `PYLOGGLE_TEST_CLICKHOUSE_DSN=clickhouse://default:@127.0.0.1:9000/default go test -tags integration ./internal/storage/clickhouse/`.
They cover a fresh install, the 0.1 → 0.2 upgrade (rebuild and in-place), retention, the whole
ingest-to-query path and saved views.

Layout: `cmd/ingestd` (main), `internal/ingest` (pipeline), `internal/issue` (fingerprints,
regression), `internal/logquery` (search syntax), `internal/otlp`, `internal/storage/clickhouse`
(client, migrations, queries), `internal/webui` (JSON API + SPA host), `web/` (React app),
`python-sdk/`, `deploy/`, `docs/`.

## License

See `LICENSE`.
