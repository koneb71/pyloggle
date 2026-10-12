# Upgrading to 1.0

Pyloggle 1.0 is the first open-source release. Coming from 0.3, your data, projects, keys and
alert rules carry over; the first start adds one table. A few defaults became safer, and a
server that relied on the old ones needs a setting or two. Read this page before you upgrade.

Coming from 0.2 or 0.1? Upgrade to 0.3 first: see [upgrading to 0.3](upgrading-0.3.md) and
[upgrading to 0.2](upgrading-0.2.md).

## Before you start

- **Back up ClickHouse** if the data matters. The new migration only adds a table and two
  columns, but a backup makes going back trivial.
- **Check how people sign in** (below). This is the change most likely to stop a server from
  starting.
- **Check your scripts** that read through `/v1/q` or the 0.1 read endpoints without an
  access key (below).

## What the first start does

Migration `016_alert_once_and_recap` creates `alert_notified`, where alert rules record what
they have already told you, and adds `send_hour` and `timezone` to `alert_rules` for the new
recap rules. Nothing else changes in the database.

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
- The Go module is now `github.com/koneb71/pyloggle`. This matters only if you build from
  source with your own flags or keep a fork.
- `ingestd -version` prints the version; the server logs it when it starts.

## What is new

- **Each alert is sent once.** A new problem alerts each rule once; a problem that comes back
  alerts again only after it was resolved; a jump alerts at most once a day; "too many errors"
  and search rules alert when it starts and again only after things went back to normal. See
  [alerting](alerting.md#how-often-you-hear-about-it).
- **A recap of yesterday**, as a new kind of alert rule, at the hour and in the time zone you
  choose.
- **Documentation** at [koneb71.github.io/pyloggle](https://koneb71.github.io/pyloggle/), and a
  **Help** page in the app.

Right after the upgrade, problems already inside a rule's time window may alert one last time,
because nothing has been recorded for them yet.

## License

Pyloggle is now open source under the Apache License 2.0, server and Python package alike. The
Python package was marked MIT before but never published under it.

## Python package

`pip install -U pyloggle` installs 1.0.0. It needs no code change.

## Rolling back

To go back to 0.3, run the 0.3 server against the same database. It ignores the new table and
columns. Alerts sent while 1.0 ran are kept; recap rules are skipped by 0.3.
