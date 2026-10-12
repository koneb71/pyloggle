# Configuration

The Pyloggle server (`ingestd`) is configured entirely through environment variables. This page
lists every one of them with its default. A test in the repository compares this page with the
code, so a setting cannot be added without appearing here.

Most servers only need a few: `CLICKHOUSE_DSN`, the [sign-in](#sign-in) settings,
`PYLOGGLE_PUBLIC_URL`, and the [alerting](#alerts) settings for the channels you use.

## How values are read

- **Numbers** must be whole numbers above zero, except where a row says otherwise. Ratios and
  rates (`INGEST_SAMPLE_*`, `INGEST_RATE_LIMIT_*`) accept decimals and zero.
- **Durations** use Go's format: `500ms`, `30s`, `5m`, `1h30m`.
- **Yes/no** values accept `true`, `false`, `1`, `0`, `yes`, `no`, `on` and `off`.
- **Lists** are separated by commas: `a@example.com,b@example.com`.
- A value that cannot be read is ignored and the default applies, so check the startup log
  after a change. Sign-in settings are the exception: a server whose sign-in is not set up
  correctly refuses to start and says what is missing.

## Server and database

| Variable | Default | What it does |
|---|---|---|
| `CLICKHOUSE_DSN` | `clickhouse://localhost:9000/pyloggle?dial_timeout=10s&read_timeout=20s` | Where ClickHouse is, in the native protocol (port 9000): `clickhouse://user:password@host:9000/database`. The database must exist; Pyloggle creates its tables in it. |
| `INGEST_PORT` | `8080` | The port the server listens on, for everything: data from your apps, the app, the API and `/metrics`. |
| `PYLOGGLE_PUBLIC_URL` | | The address people use to reach Pyloggle, such as `https://logs.example.com`. Links in alerts and the addresses on the Projects page use it. Without it, alerts carry no links and the Projects page uses the address in your browser. |
| `PYLOGGLE_DOCS_URL` | `https://koneb71.github.io/pyloggle/` | Where the app's **Help** page and "Learn more" links point. Set it to an internal copy of the docs, or to an empty value to hide the links. |
| `PYLOGGLE_AUTO_MIGRATE` | `true` | Create and update the database tables when the server starts. Turn it off only if you apply the schema yourself. |
| `PYLOGGLE_MIGRATE_REBUILD` | `true` | Only for upgrades from 0.1: rebuild the tables with one partition per day, copying the data still inside the retention window. `false` changes them in place instead. |

## Sign-in

Who can open the app. The [sign-in guide](../self-hosting/sign-in.md) walks through each
provider.

| Variable | Default | What it does |
|---|---|---|
| `PYLOGGLE_UI_AUTH` | see below | How people sign in: `oidc` (any OpenID Connect provider, such as Google, Okta, Keycloak or Authentik), `azure` (Microsoft Entra ID) or `disabled` (no sign-in, for a trusted network only). When it is not set, `PYLOGGLE_AZURE_TENANT_ID` means `azure` and `PYLOGGLE_OIDC_ISSUER` means `oidc`. With neither, or both, the server does not start. |
| `PYLOGGLE_UI_SESSION_SECRET` | | Signs the sign-in cookies. Required with `oidc` and `azure`. At least 32 characters, or base64 for 16 bytes or more: `openssl rand -base64 32`. Changing it signs everyone out. |
| `PYLOGGLE_UI_ADMINS` | | E-mail addresses (or user names) of the people who may change projects, access keys and alert rules, and open the System page. When empty, everyone who can sign in may. |
| `PYLOGGLE_OIDC_ISSUER` | | The provider's issuer URL. Pyloggle reads `<issuer>/.well-known/openid-configuration`. |
| `PYLOGGLE_OIDC_CLIENT_ID` | | The client ID of the app you registered with the provider. |
| `PYLOGGLE_OIDC_CLIENT_SECRET` | | Its client secret. |
| `PYLOGGLE_OIDC_SCOPES` | `openid profile email` | The scopes to ask for. |
| `PYLOGGLE_OIDC_REDIRECT_URL` | from the request | Where the provider sends people back to: `https://<your host>/ui/oauth2/callback`. Set it when the server sits behind a proxy that changes the host or the scheme. |
| `PYLOGGLE_OIDC_DISPLAY_NAME` | `SSO` | The name on the sign-in button: "Sign in with Okta". |
| `PYLOGGLE_OIDC_ALLOWED_DOMAINS` | | Only let in people whose e-mail ends in one of these domains. Works with `azure` too. |
| `PYLOGGLE_OIDC_ALLOWED_EMAILS` | | Only let in these e-mail addresses. Works with `azure` too. When both lists are set, matching either one is enough. |
| `PYLOGGLE_AZURE_TENANT_ID` | | Your Microsoft Entra tenant (directory) ID. Only people from this tenant can sign in. |
| `PYLOGGLE_AZURE_CLIENT_ID` | | The application (client) ID of the app registration. |
| `PYLOGGLE_AZURE_CLIENT_SECRET` | | Its client secret. |
| `PYLOGGLE_AZURE_REDIRECT_URL` | from the request | Like `PYLOGGLE_OIDC_REDIRECT_URL`, for `azure`. |

## Projects and access

| Variable | Default | What it does |
|---|---|---|
| `PYLOGGLE_DEFAULT_PROJECT_ID` | `default` | The project created on first start, and the one that receives data sent without an access key. Set it to an empty value (`PYLOGGLE_DEFAULT_PROJECT_ID=`) to require a key for everything. While sign-in is off, reading without a key also uses this project. |
| `PYLOGGLE_DEFAULT_SERVICE` | `unknown` | The app name for data that does not say which app sent it. |
| `PYLOGGLE_DEFAULT_ENV` | `prod` | The environment for data that does not say where it ran. |
| `INGEST_SHARED_SECRET` | | From 0.1: a single key, shared by all apps, that sends to the default project. Prefer per-project access keys. |
| `PYLOGGLE_ADMIN_TOKEN` | | Turns on the 0.1 admin endpoints (`/v1/projects`, `/v1/alerts/rules`, `/v1/issues/state`) for requests with this value in `X-Admin-Token`. Leave it empty unless old scripts need them. |
| `PYLOGGLE_METRICS_TOKEN` | | When set, `GET /metrics` requires `Authorization: Bearer <token>`. |
| `PYLOGGLE_EXPORT_MAX_ROWS` | `100000` | The most rows one CSV or NDJSON export from Logs may contain. |

## Receiving data

| Variable | Default | What it does |
|---|---|---|
| `INGEST_RATE_LIMIT_RPS` | `2000` | How many events per second each app may send before the rest is dropped (counted as `rate_limit`). `0` turns the limit off. A project's *Messages per second* setting replaces it for that project. |
| `INGEST_RATE_LIMIT_BURST` | `4000` | How many events an app may send at once, above the steady rate. |
| `INGEST_MAX_QUEUE_SIZE` | `10000` | Events waiting to be written to ClickHouse. When the queue is full, new events are dropped (counted as `queue_full`). |
| `INGEST_MAX_STREAMS` | `200` | Each app in each project gets its own queue, so one noisy app cannot hold up the others. Above this many, the rest share one queue. |
| `INGEST_MAX_QUEUE_PER_STREAM` | `2000` | The size of each of those queues. |
| `INGEST_BATCH_SIZE` | `500` | Rows written to ClickHouse in one go. |
| `INGEST_BATCH_TIMEOUT` | `5s` | The longest an event waits before its batch is written, even if the batch is not full. |

## Sampling

Errors are always kept. Other messages can be thinned out to save space, by severity.

| Variable | Default | What it does |
|---|---|---|
| `INGEST_SAMPLE_DEBUG` | `0.1` | The share of `debug` messages to keep, from `0` (none) to `1` (all). |
| `INGEST_SAMPLE_INFO` | `1.0` | The share of `info` messages to keep. |
| `INGEST_SAMPLE_WARN` | `1.0` | The share of `warn` messages to keep. |
| `INGEST_SAMPLE_BY_REQUEST` | `true` | Decide once per request, so a request whose messages are kept keeps all of them. |

## Retention

How many days each kind of data is kept. A change applies when the server next starts. See
[Retention and footprint](../self-hosting/retention-and-footprint.md).

| Variable | Default | What it does |
|---|---|---|
| `PYLOGGLE_RETENTION_LOGS_DAYS` | `30` | Messages. |
| `PYLOGGLE_RETENTION_ERRORS_DAYS` | `30` | Each time an error happened, with its full report. |
| `PYLOGGLE_RETENTION_SPANS_DAYS` | `7` | Request steps (spans), shown on the Requests page. |
| `PYLOGGLE_RETENTION_METRICS_DAYS` | `30` | OpenTelemetry metrics. |
| `PYLOGGLE_RETENTION_STATS_DAYS` | `365` | The counts behind the charts and lists: issue counts, messages per minute, request statistics. |

## OpenTelemetry

| Variable | Default | What it does |
|---|---|---|
| `OTLP_STORE_SPANS` | `true` | Keep the request steps (spans) sent through OpenTelemetry. Errors and requests found in them are kept either way. |
| `OTLP_STORE_SERVER_SPANS` | `true` | Also record each incoming request (a server span) as a message, so OpenTelemetry requests appear on Logs and in the request charts like the ones the Python package reports. |
| `OTLP_STORE_METRICS` | `true` | Keep the metrics sent through OpenTelemetry. |

## Alerts

Alert rules are created in the app. These settings turn the checks on and connect the
channels the rules can use. The [alerting guide](../self-hosting/alerting.md) explains each
channel.

| Variable | Default | What it does |
|---|---|---|
| `ALERTING_ENABLED` | `false` | Check the alert rules. Without it, rules can be created but never fire. |
| `ALERTING_POLL_INTERVAL` | `30s` | How often the rules are checked. |
| `ALERTING_EVAL_DELAY` | `30s` | Each check looks at data up to this long ago, so events still on their way are not missed. |
| `ALERT_SMTP_HOST` | | The mail server for e-mail alerts. E-mail is off while this is empty. |
| `ALERT_SMTP_PORT` | `587` | Its port. |
| `ALERT_SMTP_USERNAME` | | The user name, if the server needs one. |
| `ALERT_SMTP_PASSWORD` | | The password. |
| `ALERT_SMTP_FROM` | `ALERT_SMTP_USERNAME` | The sender address, such as `alerts@example.com`. |
| `ALERT_SMTP_TLS` | `false` | Connect with TLS from the start (usually port 465). When `false`, the connection is upgraded with STARTTLS if the server offers it. |
| `ALERT_SLACK_WEBHOOK_URL` | | A Slack incoming webhook, used by rules that send to Slack without their own webhook. |
| `ALERT_TELEGRAM_BOT_TOKEN` | | The token of your Telegram bot, from @BotFather. Telegram is off while this is empty. |
| `ALERT_TELEGRAM_PARSE_MODE` | `HTML` | `HTML` sends formatted messages with links. `none` sends plain text. A message Telegram cannot read is sent again once as plain text. |
| `ALERT_TELEGRAM_TIMEOUT` | `5s` | How long to wait for Telegram. |
| `ALERT_TELEGRAM_API_URL` | `https://api.telegram.org` | The Bot API address, for a self-hosted Bot API server. |
| `ALERT_WEBHOOK_TIMEOUT` | `5s` | How long to wait for a webhook to answer. |

## Process

| Variable | Default | What it does |
|---|---|---|
| `GOMEMLIMIT` | none (`200MiB` in the Compose file) | Go's soft memory limit. The server cleans up memory harder as it gets close, which keeps it under a container's memory limit. Set it to about 80% of that limit. |

## Command line

`ingestd` takes no flags apart from these two:

- `ingestd -version` prints the version and exits.
- `ingestd -help` prints the version and a pointer to this page.
