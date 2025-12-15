## Pyloggle

Lightweight Signoz-inspired **logging + error tracking** focused on Python (FastAPI, Flask, scripts).

### Run (single VM)

Start ClickHouse + ingestd:

```bash
cd deploy
docker compose up --build
```

Ingest endpoint: `http://localhost:8080/v1/ingest`

Web UI: `http://localhost:8080/ui/`

ClickHouse (from host):

- native: `localhost:9000`
- http: `localhost:8123`
- user: `pyloggle`
- password: `pyloggle`

### UI auth (Azure OIDC)

The server-rendered UI under `/ui/` supports:

- **Local dev bypass**: set **`PYLOGGLE_UI_AUTH=disabled`** (this is the default in `deploy/docker-compose.yml`).
- **Azure single-tenant login**: set **`PYLOGGLE_UI_AUTH=azure`** (default) plus:
  - **`PYLOGGLE_AZURE_TENANT_ID`**: your Entra tenant ID (GUID)
  - **`PYLOGGLE_AZURE_CLIENT_ID`**
  - **`PYLOGGLE_AZURE_CLIENT_SECRET`**
  - **`PYLOGGLE_UI_SESSION_SECRET`**: cookie signing secret (>= 32 chars, or base64 for >= 16 bytes)
  - **`PYLOGGLE_AZURE_REDIRECT_URL`** (optional): full callback URL (e.g. `https://your-host/ui/oauth2/callback`)

### Python SDK (dev install)

```bash
cd python-sdk
python -m pip install -e .
```

### Create a project + API key (local dev)

Pyloggle is **multi-tenant**. Each project has an **API key** that is shown **only once** when created/rotated (the server stores only a hash).

- **UI**: open `http://localhost:8080/ui/projects`, create a project, then copy the API key shown in the banner.
  - If you check **“set active in UI”**, the UI will store the key in an HttpOnly cookie so the UI can query the project right away.

### Python SDK (examples)

Set env vars (use the key you just copied):

```bash
export PYLOGGLE_INGEST_URL="http://localhost:8080/v1/ingest"
export PYLOGGLE_API_KEY="YOUR_PROJECT_API_KEY"
export PYLOGGLE_PROJECT_ID="default"   # project id you created in /ui/projects
```

Open the UI and keep it handy:

- `/ui/logs` for application + access logs
- `/ui/issues` for errors (created by hitting the `/boom` endpoints below)

Run script example (from repo root):

```bash
python examples/script.py
```

Run Flask example:

```bash
python examples/flask_app.py
```

Generate logs + an error (in another terminal):

```bash
curl -sS "http://127.0.0.1:5050/hello"
curl -sS "http://127.0.0.1:5050/boom"
```

Run FastAPI example:

```bash
python -m pip install "fastapi>=0.100" "uvicorn[standard]>=0.23"
python -m uvicorn examples.fastapi_app:app --reload --port 5051 --access-log --log-level info
```

Generate logs + an error:

```bash
curl -sS "http://127.0.0.1:5051/hello"
curl -sS "http://127.0.0.1:5051/boom"
```

### API auth (important)

- **Project APIs (`/v1/ingest`, `/v1/logs`, `/v1/issues`, ...)**: send header **`X-API-Key: <project api key>`**
  - If **`PYLOGGLE_DEFAULT_PROJECT_ID`** is set (default: `default`), requests **without** `X-API-Key` are allowed and will use that default project.
  - If you want to **require** keys always, set **`PYLOGGLE_DEFAULT_PROJECT_ID=""`**.
- **Admin APIs**: require header **`X-Admin-Token: <token>`** and env **`PYLOGGLE_ADMIN_TOKEN`** must be set on the server.

Query APIs (v1):

- `GET /v1/issues`
- `GET /v1/issues/occurrences?issue_id=...`
- `GET /v1/logs`
- `POST /v1/issues/state` (admin)
- `GET|POST /v1/alerts/rules` (admin)
- `GET|POST|DELETE /v1/saved-searches` (POST/DELETE admin)

### Alerting (threshold rules)

Alert rules are stored in ClickHouse (`alert_rules`) and evaluated by `ingestd` on a poll interval.

Enable the runner via env:

- **`ALERTING_ENABLED`**: `true|false` (default `false`)
- **`ALERTING_POLL_INTERVAL`**: e.g. `30s`

Notifiers:

- **Email (SMTP)**: set `ALERT_SMTP_HOST` (and optionally `ALERT_SMTP_PORT`, `ALERT_SMTP_USERNAME`, `ALERT_SMTP_PASSWORD`, `ALERT_SMTP_FROM`, `ALERT_SMTP_TLS`)
- **Webhook**: configure per-rule `webhook_urls` (timeout via `ALERT_WEBHOOK_TIMEOUT`, default `5s`)
- **Telegram**: set `ALERT_TELEGRAM_BOT_TOKEN` and configure per-rule `telegram_chat_ids` (timeout via `ALERT_TELEGRAM_TIMEOUT`, optional `ALERT_TELEGRAM_PARSE_MODE`)

Admin API (requires `PYLOGGLE_ADMIN_TOKEN`):

- `GET /v1/alerts/rules?project_id=...`
- `POST /v1/alerts/rules` with body `{"rule": {...}}`

### What you’ll see for errors

Pyloggle stores stack frames as `(filename, lineno, function, context, in_app)` so you can always see the exact **filename** and line that caused the error.