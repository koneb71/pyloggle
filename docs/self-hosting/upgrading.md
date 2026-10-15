# Upgrading to 1.0

Pyloggle 1.0 is the first open-source release. Coming from 0.3, your data, projects, keys and
alert rules carry over; the first start adds one table. A few defaults became safer, and a
server that relied on the old ones needs a setting or two. Read this page before you upgrade.

Coming from 0.2 or 0.1? Upgrade to 0.3 first: see [upgrading to 0.3](upgrading-0.3.md) and
[upgrading to 0.2](upgrading-0.2.md).

## Before you start

- **Back up ClickHouse** if the data matters. The new migrations only add a table and three
  columns, but a backup makes going back trivial.
- **Check how people sign in** (below). This is the change most likely to stop a server from
  starting.
- **Check your scripts** that read through `/v1/q` or the 0.1 read endpoints without an
  access key (below).

## What the first start does

Migration `016_alert_once_and_recap` creates `alert_notified`, where alert rules record what
they have already told you, and adds `send_hour` and `timezone` to `alert_rules` for the new
recap rules. Migration `017_logs_http_body` adds `http_body` to `logs_raw`, so messages about
web requests keep the request's body, and recreates the view that estimates each project's
storage so it counts bodies. Migration `018_request_counts` adds `request_counts_in`, which
stores nothing and feeds the request statistics, the new `request_usage_hourly` and the list of
versions; it replaces the view that built request statistics from `logs_raw` and fills the new
table from the request rows still kept. Nothing else changes in the database.

## Changes that need your attention

### Sign-in must be chosen

0.3 sent everyone to a Microsoft sign-in when `PYLOGGLE_UI_AUTH` was not set, and treated an
unknown value, such as a typo, the same way. 1.0 does not guess:

- With `PYLOGGLE_AZURE_TENANT_ID` set, the mode is `azure`, as before.
- With `PYLOGGLE_OIDC_ISSUER` set, the mode is `oidc`.
- With neither, or both, or an unknown value, **the server does not start**, and its log says
  what to set.

Set `PYLOGGLE_UI_AUTH` explicitly to `azure`, `oidc` or `disabled`. The server also checks at
start that the chosen mode has its settings and a usable session secret, instead of failing at
the first sign-in. See [sign-in](sign-in.md).

### Reading data needs a key while sign-in is on

In 0.3, a request without an access key could read the default project through `/v1/q` and the
0.1 read endpoints (`GET /v1/issues`, `/v1/logs`, …) even on a server with sign-in. In 1.0 it
gets `401 api_key_required`. Give such scripts a key with the *View* permission:

```bash
curl -H "X-API-Key: $PYLOGGLE_API_KEY" https://logs.example.com/v1/q/issues
```

Sending data without a key is unchanged: it goes to the default project.

### An empty `PYLOGGLE_DEFAULT_PROJECT_ID` now means "no default project"

0.3 ignored `PYLOGGLE_DEFAULT_PROJECT_ID=` (empty) and used `default` anyway. 1.0 takes it at
its word: there is no default project, and every request needs an access key. Leave the
variable out to keep the `default` project.

### Docker Compose publishes the port on this machine only

`deploy/docker-compose.yml` now publishes port 8080 as `127.0.0.1:8080`, and runs the
published image `ghcr.io/koneb71/pyloggle` (set `PYLOGGLE_VERSION` to pin one). If other
machines reached Pyloggle on port 8080 directly, put a reverse proxy in front of it as
[deployment](deployment.md#https-with-a-reverse-proxy) shows, or publish the port again in a
`docker-compose.override.yml`. Turn sign-in on before you do.

### Smaller changes

- `PYLOGGLE_UI_POLL_SECONDS` is gone. It had no effect since 0.2.
- The first migration no longer creates a database named `pyloggle` when `CLICKHOUSE_DSN`
  names another one. If your DSN uses a different database, an empty `pyloggle` database may
  be left over from earlier versions; check that it is empty before you drop it.
- The Go module is now `github.com/koneb71/pyloggle`, and building from source needs Go 1.26.
  This matters only if you build from source with your own flags or keep a fork.
- `ingestd -version` prints the version; the server logs it when it starts.

## What is new

- **Each alert is sent once.** A new problem alerts each rule once; a problem that comes back
  alerts again only after it was resolved; a jump alerts at most once a day; "too many errors"
  and search rules alert when it starts and again only after things went back to normal. See
  [alerting](alerting.md#how-often-you-hear-about-it).
- **A recap of yesterday**, as a new kind of alert rule, at the hour and in the time zone you
  choose.
- **Request bodies** on issues, messages and request timelines, and **Clear all messages** for
  admins, on Logs and in a project's Danger zone. Clearing keeps request counts.
- **Analytics**: errors opened and fixed, time to fix and versions compared; usage by page,
  endpoint, person and version; and the chart builder from Logs, over messages, errors or
  requests, with charts pinned to Dashboards. See [Analytics](../app/analytics.md).
- **Requests counted, not listed.** Request statistics now come from counts: exact even when
  sampling drops lines, and kept apart from Logs.
- **Documentation** at [koneb71.github.io/pyloggle](https://koneb71.github.io/pyloggle/), and a
  **Help** page in the app.

Right after the upgrade, problems already inside a rule's time window may alert one last time,
because nothing has been recorded for them yet.

## License

Pyloggle is now open source under the Apache License 2.0, server and Python package alike. The
Python package was marked MIT before but never published under it.

## Python package

`pip install -U pyloggle` installs 1.0.0. It needs no code change. It records request bodies
by default, with secret fields blanked out and files left out; pass `emit_request_body=False`
to `pyloggle.init()` to keep them out, as 0.3 did. It also counts every web request, sending a
small tally per endpoint and minute to `<ingest_url>/requests`; pass `count_requests=False` to
stop. A proxy in front of Pyloggle that only lets `/v1/ingest` through must let
`/v1/ingest/requests` through too.

## Rolling back

To go back to 0.3, run the 0.3 server against the same database. It ignores the new tables and
columns, and answers the Python package's request counts with `404`, which turns counting off;
request statistics then come only from request lines again, as before. Alerts sent while 1.0 ran are kept; recap rules are skipped by 0.3.
