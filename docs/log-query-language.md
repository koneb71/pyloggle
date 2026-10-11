# Log query language

The Logs explorer, `GET /ui/api/logs*` and `GET /v1/q/logs*` share one search syntax. It is
parsed server-side (`internal/logquery`) into parameterized ClickHouse SQL: field names come from
an allow-list and values are always bound as parameters, so there is no injection surface.

```
service:api env:prod level:(error OR warn) -logger:werkzeug "timed out" @user_id:42
http.status:>=500 duration:>250 route:/orders* tags.team:core attrs.order_id:*
```

## Rules

| Form | Meaning |
|---|---|
| `term term` | terms are AND-ed |
| `a OR b` | `OR` between terms starts an alternative; `(x OR y)` inside a value lists alternatives for one field |
| `-term` | negation (`-level:debug`, `-"health check"`) |
| `field:value` | exact match (case-sensitive on strings; levels are canonicalised) |
| `field:"two words"` | quoted values and phrases; `\"` escapes a quote |
| `field:prefix*` | wildcard: `*` matches anything (`route:/api/*`, `msg:*timeout*`) |
| `field:>N` `>=N` `<N` `<=N` | comparisons on numeric fields (`http.status`, `duration_ms`) |
| `field:*` | the field (or attribute) exists and is non-empty |
| `word` / `"a phrase"` | case-insensitive substring match on the message; a whole word uses the token index |
| `attrs.key:value`, `@key:value` | custom attribute; `tags.key:value` for tags; unknown keys fall back to attrs |

Errors come back with a byte position (`{"error":"bad_query","message":"…","pos":17}`) and the
UI underlines them.

## Fields

| Field | Aliases | Notes |
|---|---|---|
| `service` | | |
| `env` | `environment` | |
| `level` | `status`, `severity` | `warn` also matches `warning`; `fatal` matches `critical`; `error` matches `err` |
| `logger` | | |
| `hostname` | `host` | |
| `request_id` | `request` | bloom-filter indexed |
| `trace_id` | `trace` | bloom-filter indexed |
| `span_id` | `span` | |
| `release` | `version` | |
| `issue_id` | `issue` | the issue an error line belongs to |
| `event_id` | `event` | |
| `source` | | `sdk`, `otlp`, `otlp-span` |
| `http.method` | `method` | |
| `http.route` | `route` | |
| `http.status` | `status_code`, `http.status_code` | numeric |
| `http.url` | `url` | |
| `duration_ms` | `duration`, `latency`, `latency_ms` | numeric |
| `user` | `user_id`, `user.id` | hashed user id |
| `exception` | `exception.type`, `error.type` | exception class name |
| `message` | `msg` | explicit message match (same as a bare word) |
| `attrs.<key>` | `@<key>` | any attribute |
| `tags.<key>` | | any tag |

`GET /ui/api/logs/fields` returns the live list, the facet fields and the histogram group-by
options.

## Examples

```
level:error service:checkout                     errors from one service
-level:(debug OR info)                           warnings and worse
"connection reset"                               phrase anywhere in the message
msg:connect*                                     message starting with "connect"
http.status:>=500 http.route:/api/*              server errors under /api
duration:>1000 -route:/health                    slow requests, health checks excluded
request_id:4f1c…                                 every line of one request
issue:360b70…                                    every log line attached to one issue
@order_id:* tags.team:core                       lines carrying an order id, tagged team=core
release:2.3.0 OR release:2.3.1                   either release
```

## Endpoints that accept `q`

| Route | Purpose |
|---|---|
| `GET /logs?q&from&to&limit&cursor` | rows newest first; `cursor` from `next_cursor` pages older; `after=<ts>` returns newer rows (live tail) |
| `GET /logs/histogram?q&from&to&group_by&buckets` | stacked timeline; the server picks a bucket from 10 s to 7 d |
| `GET /logs/facets?q&from&to&fields&attr_keys&key` | value counts per field, top attribute/tag keys, values of one key |
| `GET /logs/patterns?q&from&to&limit` | message templates (numbers, hex, uuids and quoted strings collapsed) with counts |
| `GET /logs/context?event_id&before&after` | surrounding lines of one event |

Routes are relative to `/ui/api` (session cookie) or `/v1/q` (`X-API-Key`). Time bounds accept
RFC 3339, `YYYY-MM-DD HH:MM`, unix seconds/milliseconds, `now` and relative forms like `-15m`,
`-4h`, `-7d`.
