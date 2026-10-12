# Webhook payload

An alert rule that sends to a webhook makes an HTTP `POST` to each of its webhook addresses,
with a JSON body and `Content-Type: application/json`. Any `2xx` answer counts as delivered.
The server waits `ALERT_WEBHOOK_TIMEOUT` (5 seconds by default) for an answer. When no channel
of the rule could deliver, nothing is recorded as sent, so the next check tries again.

The examples on this page were sent by a real Pyloggle server loaded with the demo data from
[`scripts/demo_data.py`](https://github.com/koneb71/pyloggle/blob/main/scripts/demo_data.py),
with long lists shortened. A test in the repository checks that the examples and the tables
below match the code.

## What a payload never contains

The payload says what happened and how many people it affected, never who they are. It holds
no user IDs or e-mail addresses, no request addresses (only the route, such as `POST /charge`),
no assignees, and none of the rule's notification targets. The steps before an error are left
out too, because they are free text your app wrote; they appear only in e-mail, Slack and
Telegram messages, which go to the people the rule names.

## Example: too many errors

```json
{
  "rule": {
    "id": "aeebfd4afea66d6efadcf71cbcfc1633",
    "project_id": "default",
    "name": "Billing errors",
    "service": "billing",
    "env": "production",
    "condition_type": "error_rate",
    "threshold": 0.002,
    "time_window_seconds": 10800
  },
  "fired_at": "2026-10-12T17:26:58.749267717Z",
  "window_start": "2026-10-12T14:26:58.749267717Z",
  "window_end": "2026-10-12T17:26:58.749267717Z",
  "kind": "error_rate",
  "error_count": 46,
  "error_rate": 0.0042592592592592595,
  "url": "https://logs.example.com/ui/logs?from=2026-10-12T14%3A26%3A58Z&q=service%3Abilling+env%3Aproduction+level%3Aerror&to=2026-10-12T17%3A26%3A58Z",
  "context": {
    "title": "Too many errors",
    "summary": "46 errors in 3 hours, 15.3 an hour. The limit is 7.2 an hour.",
    "scope": "billing · production",
    "window": "Oct 12, 14:26–17:26 UTC (3 hours)"
  },
  "top_issues": [
    {
      "issue_id": "fcbc4ae386492a60220c54ac301ce90833f69513d13a1d8d1dc6fd7f6f374104",
      "title": "CardNetworkTimeout: card network did not answer within 10s",
      "reason": "most_frequent",
      "count": 46,
      "users": 43,
      "service": "billing",
      "env": "production",
      "release": "2.3.0",
      "where": "app/payments.py, line 131, in charge",
      "code": "resp = network.authorize(card, amount, timeout=10)",
      "request": "POST /charge → 500",
      "event_id": "d8752c00e80162b89f8b987b31bba729",
      "at": "2026-10-12T17:25:42.724Z",
      "url": "https://logs.example.com/ui/issues/fcbc4ae386492a60220c54ac301ce90833f69513d13a1d8d1dc6fd7f6f374104?event=d8752c00e80162b89f8b987b31bba729",
      "logs_url": "https://logs.example.com/ui/logs?from=2026-10-12T17%3A10%3A42Z&q=request%3Areq-3af95790679c&to=2026-10-12T17%3A30%3A42Z"
    }
  ]
}
```

## Fields in every payload

| Field | Type | What it is |
|---|---|---|
| `rule` | object | The rule that fired (below) |
| `kind` | string | What happened: `error_rate`, `issue_spike`, `new_issue`, `regression`, `log_query` or `daily_recap` |
| `fired_at` | time | When the alert was sent |
| `window_start`, `window_end` | time | The time range the rule looked at. For a recap, yesterday in the rule's time zone |
| `url` | string | The matching page in Pyloggle. Links are only filled in when `PYLOGGLE_PUBLIC_URL` is set |
| `context` | object | The alert in words, as the e-mail, Slack and Telegram messages say it (below) |
| `top_issues` | array | Up to three issues behind the alert, each told through the latest time it happened (below). Not sent for `log_query` and `daily_recap` |

Times are RFC 3339 in UTC.

### `rule`

| Field | Type | What it is |
|---|---|---|
| `id` | string | The rule's ID |
| `project_id` | string | The project it belongs to |
| `name` | string | The rule's name |
| `service`, `env` | string | The app and environment it watches, when it is limited to one |
| `http_route`, `http_method` | string | The endpoint it watches, when it is limited to one |
| `condition_type` | string | The kind of rule; the same values as `kind` |
| `query` | string | For `log_query`: the search |
| `comparison` | string | For `log_query`: `gt`, `gte`, `lt`, `lte` or `eq` |
| `threshold` | number | The limit. For `error_rate`, errors per second; for `issue_spike`, how many more times than before; for `log_query`, a number of messages |
| `time_window_seconds` | number | How far back the rule looks |

### `context`

| Field | Type | What it is |
|---|---|---|
| `title` | string | What happened, in a few words: "Too many errors" |
| `summary` | string | One or two sentences with the numbers |
| `scope` | string | Which apps and environments: "billing · production" or "all apps · all environments" |
| `window` | string | The time range in words |

### `top_issues[]`

| Field | Type | What it is |
|---|---|---|
| `issue_id` | string | The issue's ID |
| `title` | string | The error type and message |
| `reason` | string | Why it is listed: `jumped`, `new`, `came_back` or `most_frequent` |
| `count` | number | How many times it happened in the window |
| `prev_count` | number | For `jumped`: how many times in the window before |
| `users` | number | How many different people it affected |
| `service`, `env`, `release` | string | Where the latest time happened |
| `where` | string | The file, line and function in your code |
| `code` | string | The line of code that failed |
| `request` | string | The web request's method, route and status: `POST /charge → 500` |
| `event_id` | string | The latest time it happened |
| `at` | time | When that was |
| `url` | string | That time, in Pyloggle |
| `logs_url` | string | The messages logged during the same request |
| `trace_url` | string | The request's timeline, when its steps were stored |

## Fields for each kind

### `error_rate`: too many errors

| Field | Type | What it is |
|---|---|---|
| `error_count` | number | Errors in the window |
| `error_rate` | number | Errors per second over the window |

The rule fires once when the rate reaches the limit, and again only after a check finds it
back under the limit.

### `new_issue` and `regression`: a new problem, a problem that came back

| Field | Type | What it is |
|---|---|---|
| `issues` | array | The problems, each with `issue_id`, `title`, `culprit` (where it happened), `count`, `users` and `url` |

```json
"issues": [
  {
    "issue_id": "fcbc4ae386492a60220c54ac301ce90833f69513d13a1d8d1dc6fd7f6f374104",
    "title": "CardNetworkTimeout: card network did not answer within 10s",
    "culprit": "app.payments in charge",
    "count": 46,
    "users": 43,
    "url": "https://logs.example.com/ui/issues/fcbc4ae386492a60220c54ac301ce90833f69513d13a1d8d1dc6fd7f6f374104"
  }
]
```

Each problem is sent once per rule. A problem that comes back is sent again each time it comes
back after being resolved.

### `issue_spike`: problems that jumped

| Field | Type | What it is |
|---|---|---|
| `spikes` | array | The problems that jumped, each with `issue_id`, `title`, `cur_count` (times in the window), `prev_count` (times in the window before), `delta`, `users` and `url` |

```json
"spikes": [
  {
    "issue_id": "4d2b6f7633eba4722b88ebbbe4623e94a9e1dc2ee595c67178d9a2ffd5a7d6c1",
    "title": "OperationalError: canceling statement due to statement timeout",
    "cur_count": 27,
    "prev_count": 19,
    "delta": 8,
    "users": 26,
    "url": "https://logs.example.com/ui/issues/4d2b6f7633eba4722b88ebbbe4623e94a9e1dc2ee595c67178d9a2ffd5a7d6c1"
  }
]
```

Each problem is sent at most once a day per rule.

### `log_query`: messages matched a search

| Field | Type | What it is |
|---|---|---|
| `count` | number | Messages that matched in the window |
| `samples` | array | Up to three of them, newest first, each with `ts`, `level`, `service`, `env` and `message` |

```json
"kind": "log_query",
"count": 46,
"samples": [
  {
    "ts": "2026-10-12T17:25:42.724Z",
    "level": "error",
    "service": "billing",
    "env": "production",
    "message": "card network did not answer within 10s"
  }
]
```

Like `error_rate`, it fires once when the search matches and again only after it stopped
matching.

### `daily_recap`: the recap of yesterday

`count` is yesterday's number of errors, and `recap` holds the rest.

```json
{
  "rule": {
    "id": "cad60f2cd62c3d2e06093dc1a2c47dcb",
    "project_id": "default",
    "name": "Morning recap",
    "condition_type": "daily_recap",
    "threshold": 0,
    "time_window_seconds": 86400
  },
  "fired_at": "2026-10-12T17:30:07.027682346Z",
  "window_start": "2026-10-10T22:00:00Z",
  "window_end": "2026-10-11T22:00:00Z",
  "kind": "daily_recap",
  "count": 54,
  "url": "https://logs.example.com/ui/overview?from=2026-10-10T22%3A00%3A00Z&to=2026-10-11T22%3A00%3A00Z",
  "context": {
    "title": "Yesterday in Default",
    "summary": "Oct 11: 54 errors, up from 3 the day before. 1 new problem. 0.9% of requests failed.",
    "scope": "all apps · all environments",
    "window": "Sunday, Oct 11 (Europe/Berlin)"
  },
  "recap": {
    "date": "2026-10-11",
    "timezone": "Europe/Berlin",
    "project": "Default",
    "totals": {
      "errors": 54,
      "people": 39,
      "messages": 15668,
      "apps": 4,
      "requests": 15656,
      "failed_requests": 141,
      "new_problems": 1,
      "came_back": 0
    },
    "previous": {
      "errors": 3,
      "people": 3,
      "messages": 3297,
      "apps": 3,
      "requests": 3297,
      "failed_requests": 15,
      "new_problems": 0,
      "came_back": 0
    },
    "new_issues": [
      {
        "issue_id": "ec02e23a3055e495212490bbec1e061c7dfa73506b30b44d8eb7fcaae5e139ef",
        "title": "TemplateNotFound: emails/receipt.html",
        "count": 12,
        "url": "https://logs.example.com/ui/issues/ec02e23a3055e495212490bbec1e061c7dfa73506b30b44d8eb7fcaae5e139ef"
      }
    ],
    "top_issues": [
      {
        "issue_id": "4d2b6f7633eba4722b88ebbbe4623e94a9e1dc2ee595c67178d9a2ffd5a7d6c1",
        "title": "OperationalError: canceling statement due to statement timeout",
        "count": 29,
        "people": 28,
        "url": "https://logs.example.com/ui/issues/4d2b6f7633eba4722b88ebbbe4623e94a9e1dc2ee595c67178d9a2ffd5a7d6c1"
      },
      {
        "issue_id": "a26b3675d24a031ec01116b4cf7d3d9ef041d6ddd2704003a859e8a3bf680719",
        "title": "KeyError: 'currency'",
        "count": 13,
        "people": 13,
        "url": "https://logs.example.com/ui/issues/a26b3675d24a031ec01116b4cf7d3d9ef041d6ddd2704003a859e8a3bf680719"
      }
    ],
    "apps": [
      {
        "service": "api",
        "status": "degraded",
        "failed_pct": 1.4973106556185491,
        "errors": 29,
        "last_seen": "2026-10-11T21:39:00Z"
      },
      {
        "service": "search",
        "status": "silent",
        "failed_pct": 0,
        "errors": 0,
        "last_seen": "2026-10-11T18:06:00Z"
      }
    ],
    "ongoing": [
      {
        "rule": "Billing errors",
        "kind": "error_rate",
        "since": "2026-10-12T17:26:58.876Z"
      }
    ]
  }
}
```

### `recap`

| Field | Type | What it is |
|---|---|---|
| `date` | string | Yesterday, as `YYYY-MM-DD` in the rule's time zone |
| `timezone` | string | The rule's time zone |
| `project` | string | The project's name |
| `totals` | object | Yesterday's numbers (below) |
| `previous` | object | The same numbers for the day before, for comparison |
| `new_issues` | array | Up to five problems seen for the first time yesterday |
| `came_back` | array | Up to five problems that came back yesterday after being resolved |
| `top_issues` | array | The five problems that happened most yesterday |
| `apps` | array | Apps that need a look: `failing`, `degraded` or `silent` (below) |
| `alerts` | array | How many alerts each rule sent yesterday: `rule` (its name) and `count` |
| `ongoing` | array | Count rules whose problem is still going on: `rule`, `kind` and `since` |

The three issue lists hold `issue_id`, `title`, `count` and `people` for yesterday, and `url`.

### `recap.totals` and `recap.previous`

| Field | Type | What it is |
|---|---|---|
| `errors` | number | Errors |
| `people` | number | Different people affected by errors |
| `messages` | number | Messages written |
| `apps` | number | Apps that sent anything |
| `requests` | number | Web requests |
| `failed_requests` | number | Web requests that failed with a server error |
| `new_problems` | number | Problems seen for the first time |
| `came_back` | number | Problems that came back after being resolved |

### `recap.apps[]`

| Field | Type | What it is |
|---|---|---|
| `service` | string | The app |
| `status` | string | `failing`, `degraded` or `silent` (it stopped sending) |
| `failed_pct` | number | The share of its requests that failed, in percent |
| `errors` | number | Its errors |
| `last_seen` | time | When it last sent anything |
