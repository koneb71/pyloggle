# HTTP API

Everything the app shows comes from a JSON API, and scripts can use the same API with an
access key. This page lists every route the server answers. A test in the repository compares
it with the code, so a new route cannot go undocumented.

## The doors

| Path | Used by | Access |
|---|---|---|
| `POST /v1/ingest` | The Python package | An access key with *Send data*. See the [wire format](wire-format.md). |
| `POST /v1/logs`, `/v1/traces`, `/v1/metrics` | OpenTelemetry exporters | An access key with *Send data*. See [OpenTelemetry](../send-data/opentelemetry.md). |
| `/v1/q/…` | Scripts, notebooks, other dashboards | An access key: *View* to read, *Make changes* for everything else |
| `/ui/api/…` | The app | The browser's sign-in cookie |
| `GET /metrics` | Prometheus | Open, or a bearer token when `PYLOGGLE_METRICS_TOKEN` is set |
| `GET /healthz` | Load balancers, Docker | Open |

OpenTelemetry exporters may also use the same paths under `/v1/otlp/v1/`, such as
`POST /v1/otlp/v1/traces`, for collectors that add the `/v1/<signal>` part themselves.

## Using the query API

The routes below are served twice, with the same parameters and answers: under `/v1/q` for
access keys and under `/ui/api` for the app. Examples on this page use `/v1/q`.

```bash
curl -H "X-API-Key: $PYLOGGLE_API_KEY" "https://logs.example.com/v1/q/issues?status=open&sort=users"
```

**Access keys.** Send the key as `X-API-Key: <key>` or `Authorization: Bearer <key>`. The key
decides the project. `GET` needs a key with *View*; `POST` and `DELETE` need *Make changes*.

| Answer | When |
|---|---|
| `401 unauthorized` | The key is unknown, or no key was sent and there is no default project |
| `401 api_key_required` | No key was sent for a change, or for a read while sign-in is on |
| `403 insufficient_scope` | The key lacks the permission the method needs |
| `403 admin_required` | Only people listed in `PYLOGGLE_UI_ADMINS` may do this |

Without a key, reads go to the default project (`PYLOGGLE_DEFAULT_PROJECT_ID`), but only while
sign-in is off. Changes always need a key.

**In the app.** Requests under `/ui/api` carry the sign-in cookie, and every change must also
send the header `X-Pyloggle-Request: 1`, which browsers cannot add for another site.

**Times.** `from` and `to` take relative times (`-15m`, `-4h`, `-7d`, `-1w`, `now`), a date
and time in UTC (`2026-10-12`, `2026-10-12 14:30`, `2026-10-12T14:30:00`), RFC 3339 with a
time zone (`2026-10-12T14:30:00+08:00`), or a Unix time in seconds, milliseconds,
microseconds or nanoseconds. `to` defaults to now, and `from` to a range that suits the route.

**Errors** come back as JSON with a code and a sentence:

```json
{"error": "bad_query", "message": "unbalanced parenthesis", "pos": 17}
```

`bad_query` carries the position of the mistake in `q`. While ClickHouse is not answering,
routes answer `503` with `unavailable`.

**Paging.** Lists take `limit`. Logs and requests return `next_cursor`; pass it back as
`cursor` for the next, older page.

## Your session and project

| Route | What it does |
|---|---|
| `GET /me` | Who is signed in, whether they are an administrator, the current project, and which features are on |
| `POST /login` | Sign in to the app with an access key instead of a sign-in provider: `{"api_key": "pl_…"}` |
| `POST /logout` | Sign out |
| `POST /project/select` | Switch the app to another project: `{"project_id": "…"}` |

## Issues

| Route | What it does |
|---|---|
| `GET /issues` | Issues, with counts per status. `status` is `open`, `regressed` (came back), `resolved`, `ignored` or `all`. `sort` is `last_seen` (default), `first_seen`, `count` or `users`. Filters: `q` (words in the title, message, location or error type, or an issue ID), `service`, `env`, `release`, `level`, `assignee` (`me` for yourself), `from`, `to`. `limit` (up to 500) and `offset` page. `sparklines=1` adds a 24-hour trend per issue |
| `GET /issues/assignees` | People issues are assigned to, for the assignee filter |
| `GET /issues/{id}` | One issue, with its latest report and state |
| `GET /issues/{id}/trend` | How often it happened over time: `from`, `to`, `buckets` or `bucket_seconds` |
| `GET /issues/{id}/breakdown` | How often it happened per value of `field`: `env`, `service`, `release`, `http_route`, `http_method`, `http_status`, `hostname`, `user`, `runtime_version`, `sdk_version`, `level`, `mechanism`, `handled`, or `tag:<key>` |
| `GET /issues/{id}/occurrences` | Each time it happened, newest first. `before` pages back |
| `GET /issues/{id}/occurrences/latest` | The latest time it happened, with the full report |
| `GET /issues/{id}/occurrences/{event_id}` | One time it happened, with the full report |
| `GET /events/{event_id}` | The same, found by its event ID alone |
| `GET /issues/{id}/activity` | The issue's history: status changes, assignments, comments, and times it came back |
| `POST /issues/{id}/state` | Resolve, ignore or reopen: `{"status": "resolved"}`. `status` is `open`, `resolved` or `ignored`. Add `comment`, `resolved_in_release` (a version, or `next`), or, when ignoring, `snooze_until` (RFC 3339), `snooze_for_seconds` or `snooze_count` (until it happens this many more times) |
| `POST /issues/state` | The same for several issues: add `"issue_ids": [...]` |
| `POST /issues/{id}/assign` | Assign the issue: `{"assignee": "ana@example.com"}`, or `""` for no one |
| `POST /issues/{id}/comments` | Add a comment: `{"body": "…"}` |

## Logs

All routes take `q` in the [search syntax](../app/search.md), `from` and `to`.

| Route | What it does |
|---|---|
| `GET /logs` | Messages, newest first (`order=asc` for oldest first). `limit`, `cursor` for older pages, `after=<time>` for messages newer than the last one you have |
| `GET /logs/histogram` | Messages over time, split by `group_by` (`level` by default). `buckets` or `bucket_seconds` sets the bar size |
| `GET /logs/facets` | The most common values of `fields` (a comma-separated list), the most used detail keys (`attr_keys` says how many, 30 by default), or the values of one detail (`key=attrs.order_id`) |
| `GET /logs/patterns` | Messages grouped by their shape, with counts. `limit` |
| `GET /logs/context` | The messages just before and after one message: `event_id`, `before`, `after` (how many of each) |
| `GET /logs/aggregate` | Numbers over time: `metric` is `count`, `avg`, `sum`, `min`, `max`, `p50`, `p95` or `p99`; `of` is the field to measure, such as `duration_ms` (all but `count` need it); `group_by` splits the result, `limit` (up to 50) caps the groups, `buckets` or `bucket_seconds` sets the interval |
| `GET /logs/export` | Every matching message as a file: `format=csv` (default) or `ndjson`, `cols` to pick columns, `limit` up to `PYLOGGLE_EXPORT_MAX_ROWS` |
| `GET /logs/fields` | The fields you can search and group by |
| `GET /saved-views` | Saved searches |
| `POST /saved-views` | Save a search: `{"name": "…", "query": "…", "is_public": true}`. With an `id`, changes that saved search |
| `DELETE /saved-views/{id}` | Delete a saved search |

## Requests

| Route | What it does |
|---|---|
| `GET /traces` | Requests, newest first, filtered by `q` on their steps. `limit`, `cursor`. `include=stats` adds how long requests took and the busiest operations in the range |
| `GET /traces/fields` | The fields you can search on Requests |
| `GET /traces/{trace_id}` | One request with all its steps, the errors it caused and the messages logged during it. `at` (a time near the request) makes the lookup faster |

## Overview and dashboards

These take `from`, `to`, and usually `service` and `env`.

| Route | What it does |
|---|---|
| `GET /metrics/overview` | Errors, people affected, messages, apps and new issues. `compare=previous` adds the same figures for the period just before |
| `GET /metrics/attention` | Issues that are new, came back or jumped, ranked for the Overview page |
| `GET /metrics/services` | Each app's health: OK, degraded, failing or silent |
| `GET /metrics/errors` | Errors over time |
| `GET /metrics/logs` | Messages over time, by severity |
| `GET /metrics/top-issues` | The most frequent issues in the range |
| `GET /metrics/requests` | Web requests: count, failed share, response times and the busiest endpoints. `source` is `logs` (requests reported by the Python package and OpenTelemetry) or `otel` (the `http.server.request.duration` metric). `compare=previous` as above |
| `GET /releases` | Versions of your apps, in the order they were first seen |
| `GET /metrics/names` | The OpenTelemetry metrics this project has received |
| `GET /metrics/series` | One metric over time: `name`, `agg`, `group_by`, `buckets` or `bucket_seconds`. `agg` depends on the metric's type: `avg`, `sum`, `min`, `max` or `last` for gauges; `rate` too for counters; `p50`, `p95`, `p99`, `avg`, `count` or `rate` for histograms |

## Projects and access keys

Changes here are limited to administrators when `PYLOGGLE_UI_ADMINS` is set.

| Route | What it does |
|---|---|
| `GET /projects` | All projects, with their limits and storage |
| `POST /projects` | Create a project: `{"id": "shop", "name": "Shop", "description": "", "max_events_per_second": 0, "max_storage_gb": 0, "retention_days": 0}` (0 means no limit, or the server's retention). The answer holds its first access key, shown only this once |
| `GET /projects/{id}` | One project |
| `POST /projects/{id}` | Change its name, description or limits (same fields) |
| `GET /projects/{id}/usage` | Storage per kind of data per day, for the last `days` days |
| `GET /projects/{id}/keys` | Its access keys: name, first characters, permissions, last use |
| `POST /projects/{id}/keys` | Create a key: `{"name": "checkout-api", "scopes": ["ingest"]}`. Scopes are `ingest` (*Send data*), `read` (*View*) and `write` (*Make changes*). The answer holds the key, shown only this once |
| `POST /projects/{id}/keys/{key_id}/revoke` | Disable a key. Apps using it are refused from then on |
| `POST /projects/{id}/rotate-key` | Replace the project's `default` key with a new one |
| `POST /projects/{id}/purge-user` | Delete the messages and error reports of one person: `{"user_email": "…"}` or `{"user_id_hash": "…"}`. Counts of people affected keep counting them |
| `DELETE /projects/{id}` | Delete the project with all its data, keys, rules and saved searches. It stops accepting data at once |
| `GET /admin/audit` | Who changed projects, keys and data, newest first. `project_id`, `limit` |

## Alerts

| Route | What it does |
|---|---|
| `GET /alerts/rules` | The project's alert rules |
| `POST /alerts/rules` | Create or change a rule: `{"rule": {...}}` with the fields of a rule as returned above. Leave out `id` to create one |
| `GET /alerts/rules/preview` | How many messages a search alert would have matched just now: `query`, `window_seconds`, `service`, `env`, `http_route`, `http_method` |
| `GET /alerts/rules/recap-preview` | The text of a recap of yesterday, built now: `timezone`, `service`, `env`, `name` |
| `GET /alerts/firings` | Alerts sent recently, newest first. `limit` |

The [webhook payload](webhook-payload.md) page describes what alerts send.

## The server

| Route | What it does |
|---|---|
| `GET /system` | The server's own health for the System page: database, disk, tables, queues, alerting. Administrators only |
| `GET /metrics` | Prometheus metrics: data received and dropped, queues, alert checks, ClickHouse tables and the server version. Not under `/v1/q` |
| `GET /healthz` | `ok` while the server runs. `?verbose=1` answers JSON with queue depths and drop counters |

## Sign-in pages

The app's sign-in flow uses these pages. Register the callback with your provider, as the
[sign-in guide](../self-hosting/sign-in.md) explains.

| Route | What it does |
|---|---|
| `GET /ui/login` | Starts sign-in with the configured provider |
| `GET /ui/oauth2/callback` | Where the provider sends people back: `https://<your host>/ui/oauth2/callback` |
| `GET /ui/logout` | Signs out and returns to the sign-in page |

## Endpoints from 0.1

These are kept so scripts written for Pyloggle 0.1 keep working. New scripts should use
`/v1/q`.

| Route | What it does |
|---|---|
| `GET /v1/issues` | Issues: `service`, `env`, `from`, `to`, `limit`. Access key with *View* |
| `GET /v1/logs` | Messages: `service`, `env`, `level`, `q`, `trace_id`, `request_id`, `from`, `to`, `limit`. Access key with *View* |
| `GET /v1/issues/occurrences` | Times one issue happened: `issue_id`, `from`, `to`, `limit`. Access key with *View* |
| `GET /v1/saved-searches` | Saved searches; also `POST` and `DELETE`. Access key |
| `GET /v1/metrics/errors/trend` | Errors over time: `service`, `env`, `http_route`, `http_method`, `from`, `to`, `bucket_seconds`. Access key with *View* |
| `POST /v1/issues/state` | Change an issue's status. Needs `X-Admin-Token` |
| `GET /v1/alerts/rules` | Alert rules; `POST` changes one. Needs `X-Admin-Token` |
| `GET /v1/projects` | Projects; `POST` creates one. Needs `X-Admin-Token` |
| `POST /v1/projects/update` | Change a project. Needs `X-Admin-Token` |
| `POST /v1/projects/rotate-key` | Replace a project's key. Needs `X-Admin-Token` |

The `X-Admin-Token` routes answer `403` unless `PYLOGGLE_ADMIN_TOKEN` is set.
