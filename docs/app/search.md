# Search syntax

One search language works everywhere in Pyloggle: the **Logs** page, the **Requests** page, alert
rules that watch for messages, and the [query API](#searching-from-scripts). You can type it, or
build it with clicks and let Pyloggle write it for you.

```
service:api env:prod level:(error OR warn) -logger:werkzeug "timed out" @user_id:42
http.status:>=500 duration:>250 route:/orders* tags.team:core attrs.order_id:*
```

## Searching in the app

- **Type words** to find messages that contain them: `timed out`. Put a phrase in quotes to
  match it exactly: `"payment failed"`.
- **Narrow by a field** with `field:value`, for example `service:api` or `level:error`. Each
  part you add must match, so `service:api level:error` means errors from the `api` app.
- **Click instead of typing.** *Add filter* lets you pick a field by its plain name, then a
  value from what is in the selected time range, then *is* or *is not*. Values in the filter
  sidebar and in a message's details add themselves to the search when you click them.
- **Read it back.** The search bar shows each part as a chip in plain words ("Severity is
  Error", "App is not worker"), so you can check what you asked for.
- **Get help on the spot.** The **?** button in the search bar lists examples; click one to add
  it. A typing mistake is underlined at the place it happens.
- Press `/` from anywhere on the page to jump to the search bar.

## Rules

| You write | It means |
|---|---|
| `timeout error` | Both words must appear. Every part of a search must match. |
| `a OR b` | Either part may match. `level:(error OR fatal)` lists several values for one field. |
| `-term` | Leave matches out: `-level:debug`, `-"health check"`. |
| `field:value` | The field has exactly this value. Text is case-sensitive; severities are not. |
| `field:"two words"` | A value with spaces. Use `\"` for a quote inside it. |
| `field:prefix*` | `*` stands for anything: `route:/api/*`, `msg:*timeout*`. |
| `field:>N` `>=N` `<N` `<=N` | Compare numbers, for fields such as `http.status` and `duration_ms`. |
| `field:*` | The field is present and not empty. |
| `word`, `"a phrase"` | Found anywhere in the message (on Requests: in the step name), ignoring case. |
| `attrs.key:value`, `@key:value` | A detail your app sent with the message (an attribute). |
| `tags.key:value`, `#key:value` | A tag. Tags are shown on issues too. |

A field Pyloggle does not know is treated as an attribute, so `order_id:42` is the same as
`@order_id:42`.

## Fields on the Logs page

| Field | Also written as | What it is |
|---|---|---|
| `service` | | The app that wrote the message |
| `env` | `environment` | Where it ran, such as `prod` or `staging` |
| `level` | `severity`, `status` | `debug`, `info`, `warn`, `error` or `fatal`. `warn` also finds `warning`, `fatal` finds `critical`, `error` finds `err` |
| `logger` | | The code module that wrote it |
| `hostname` | `host` | The server or container |
| `release` | `version` | Your app's version |
| `request_id` | `request` | Every message of one request |
| `trace_id` | `trace` | Every message of one request timeline |
| `span_id` | `span` | Messages written during one step |
| `issue_id` | `issue` | Errors that belong to one issue |
| `event_id` | `event` | One single message |
| `source` | | How it arrived: `sdk`, `otlp` or `otlp-span` |
| `http.method` | `method` | `GET`, `POST`, … |
| `http.route` | `route` | The route pattern, such as `/orders/{id}` |
| `http.status` | `status_code`, `http.status_code` | The response code (a number) |
| `http.url` | `url` | The full address that was requested |
| `duration_ms` | `duration`, `latency`, `latency_ms` | How long the request took, in milliseconds |
| `user` | `user_id`, `user.id` | The signed-in person (stored as a hash) |
| `exception` | `exception.type`, `error.type` | The error's class name, such as `KeyError` |
| `message` | `msg` | The message text, the same as a bare word |
| `attrs.<key>` | `@<key>` | Any attribute |
| `tags.<key>` | `#<key>` | Any tag |

## Fields on the Requests page

On the Requests page a search matches the steps of each request (spans, in OpenTelemetry
terms). Bare words match the step's name, such as `GET /orders` or `SELECT orders`.

| Field | Also written as | What it is |
|---|---|---|
| `service` | | The app that ran the step |
| `env` | `environment` | Where it ran |
| `name` | `span.name`, `operation` | The step's name |
| `kind` | `span.kind` | `server` (handles a request), `client` (calls another service), `internal`, `producer`, `consumer` |
| `status` | `span.status` | `error`, `ok` or `unset` (not reported) |
| `status_message` | | The error text a failed step reported |
| `scope` | `instrumentation`, `library` | The library that recorded the step |
| `hostname` | `host` | The server or container |
| `release` | `version` | Your app's version |
| `trace_id` | `trace` | One request timeline |
| `span_id` | `span` | One step |
| `parent_span_id` | `parent` | Steps directly inside another step |
| `http.method` | `method` | `GET`, `POST`, … |
| `http.route` | `route` | The route pattern |
| `http.status` | `status_code`, `http.status_code` | The response code (a number) |
| `duration_ms` | `duration`, `latency`, `latency_ms` | How long the step took, in milliseconds |
| `duration_us` | | The same, in microseconds |
| `attrs.<key>` | `@<key>` | Any attribute, such as `db.system` or `messaging.system` |

## Examples

On the Logs page:

```
level:error service:checkout                     errors from one app
-level:(debug OR info)                           warnings and worse
"connection reset"                               an exact phrase anywhere in the message
msg:connect*                                     messages that start with "connect"
http.status:>=500 http.route:/api/*              failed requests under /api
duration:>1000 -route:/health                    slow requests, leaving out health checks
request_id:4f1c…                                 every message of one request
issue:360b70…                                    every message attached to one issue
@order_id:* tags.team:core                       messages with an order id, tagged team=core
release:2.3.0 OR release:2.3.1                   either version
```

On the Requests page:

```
"GET /orders"                                    requests and steps with this name
service:api status:error                         failed steps in one app
duration:>250                                    steps slower than 250 ms
kind:client db.system:postgresql                 calls to the database
-service:worker                                  leave an app out
```

## Time ranges

Each page has a time picker with common ranges and a custom range, and you can drag across a
chart to zoom in. The range is part of the page address, so a link you share shows the same
messages. In the address and in the API, times can be written as:

- relative to now: `-15m`, `-4h`, `-7d`, and `now`;
- a date and time: `2026-10-12 14:30` or RFC 3339 (`2026-10-12T14:30:00Z`);
- a Unix time in seconds or milliseconds.

## Searching from scripts

The same syntax works in the `q` parameter of the query API. Send an access key with the
*View* permission in the `X-API-Key` header:

```bash
curl -H "X-API-Key: $PYLOGGLE_API_KEY" \
  "https://pyloggle.example.com/v1/q/logs?q=level:error%20service:api&from=-1h&limit=50"
```

| Route | What it returns |
|---|---|
| `GET /v1/q/logs?q&from&to&limit&cursor` | Messages, newest first. Pass `cursor` from `next_cursor` to get older ones, or `after=<time>` for newer ones (live mode) |
| `GET /v1/q/logs/histogram?q&from&to&group_by&buckets` | Counts over time, split by a field. The server picks a bar size from 10 seconds to 7 days |
| `GET /v1/q/logs/facets?q&from&to&fields&attr_keys&key` | The most common values of each field, with counts |
| `GET /v1/q/logs/patterns?q&from&to&limit` | Similar messages grouped by their shape, with counts |
| `GET /v1/q/logs/context?event_id&before&after` | The messages written just before and after one message |
| `GET /v1/q/logs/aggregate?q&from&to&…` | Counts, sums, averages and percentiles over time, split by a field |
| `GET /v1/q/logs/export?q&from&to&format` | Every matching message as CSV or NDJSON |
| `GET /v1/q/logs/fields` | The fields you can search, for autocomplete |
| `GET /v1/q/traces?q&from&to` | Requests that match, with their duration and result |
| `GET /v1/q/traces/fields` | The fields you can search on the Requests page |

A search Pyloggle cannot read comes back as `400` with the position of the mistake:
`{"error":"bad_query","message":"…","pos":17}`. The app underlines that position.

## How it is run

Pyloggle turns each search into a database query on the server. Field names come from the fixed
lists above and every value you type is passed as a separate parameter, never pasted into the
query, so a search cannot change what the server runs.
