# Wire format

This page describes what the Python SDK sends to `POST /v1/ingest`, for anyone who wants to
send data from another language or check what is stored. Apps that use OpenTelemetry send to
the [OpenTelemetry endpoints](../send-data/opentelemetry.md) instead.

## Request

- **Body:** newline-delimited JSON, one event per line, with
  `Content-Type: application/x-ndjson`. Add `Content-Encoding: gzip` to send it compressed.
- **Access key:** `X-API-Key: <key>` or `Authorization: Bearer <key>`. The key needs the *Send
  data* permission. Without a key, events go to the project named by
  `PYLOGGLE_DEFAULT_PROJECT_ID`, unless that setting is empty.
- **Size:** one line may be up to 4 MB, and a request may hold any number of lines. A line
  over 4 MB fails the whole request with `400`. The SDK sends up to 200 events per request and splits
  a batch in half when a proxy in front of the server answers `413`.

## Response

The server answers `202 Accepted` and says what it did with every line:

```json
{"accepted": 98, "rejected": 2, "errors": ["line 4: invalid character …"], "dropped": {"sampled": 10}}
```

- `rejected` lines could not be read. `errors` gives the first five reasons.
- `dropped` lines were read and then not stored on purpose, counted by reason: `sampled`,
  `rate_limit`, `queue_full` or `quota`. It is left out when nothing was dropped.

Other answers: `401` for an unknown key or a missing one, `403` for a key without the *Send
data* permission, `400` for a body that is not valid gzip or has a line that is too long.

## Decoding is lenient

Everything the 0.1 SDK and plain `logging` handlers sent is accepted:

- `ts`: RFC 3339 in any zone, zone-less ISO (assumed UTC), `YYYY-MM-DD HH:MM:SS`, unix seconds,
  milliseconds, microseconds or nanoseconds; missing or empty means "now".
- `attrs`, `tags`, `data`, `vars`, `headers`: values may be strings, numbers, booleans (kept as
  `true`/`false`), null (dropped) or objects/arrays (compact JSON text).
- `level`: `warning → warn`, `critical|fatal → fatal`, `err → error`, `trace → debug`,
  `notice → info`; anything else is kept lower-cased.
- `environment` is an alias of `env`; `http.body` may be a string or an object (stored as
  compact JSON text). The server keeps at most 64 KB of a body and marks a longer one with
  `attrs.request_body_truncated = "true"`, which the SDK also sets when it cuts one.

## Event

| Field | Type | Notes |
|---|---|---|
| `event_id` | string | 32 hex chars; generated when absent |
| `ts` | timestamp | see above |
| `service`, `env` | string | default `PYLOGGLE_DEFAULT_SERVICE` / `PYLOGGLE_DEFAULT_ENV` |
| `release`, `hostname`, `transaction` | string | optional |
| `level` | string | `debug` `info` `warn` `error` `fatal` |
| `logger`, `message` | string | |
| `trace_id`, `span_id`, `request_id` | string | hex ids; `request_id` groups a request's lines |
| `attrs` | map | searchable as `attrs.<key>` / `@<key>`; indexed keys |
| `tags` | map | searchable as `tags.<key>`; shown on issues |
| `extra` | any JSON | free-form context, stored as text |
| `fingerprint` | string[] | grouping override; `"{{ default }}"` expands to the server's fingerprint |
| `duration_ms` | number | request latency |
| `exception` | object | see below |
| `http` | object | `method`, `route`, `status`, `url`, `query`, `headers`, `body`, `user_agent`, `remote_addr`, `server_addr`, `client_ip` |
| `user` | object | `id_hash`, `id`, `email`, `username`, `ip` (raw fields are opt-in in the SDK) |
| `runtime` | object | `name`, `version`, `platform`, `pid`, `thread` |
| `sdk` | object | `name`, `version` |
| `breadcrumbs` | object[] | `ts`, `type`, `category`, `level`, `message`, `data` (newest last) |
| `mechanism` | object | `type` (`excepthook`, `flask`, `asgi`, `logging`, …), `handled` (bool) |
| `skip_issue` | bool | store the log line but do not create/attach an issue (the SDK sets it on duplicate framework error logs) |

An event becomes an **issue occurrence** when it carries an `exception` or has level `error` or
`fatal`, unless `skip_issue` is true. Every event is also stored as a log line.

## Exception

```json
{
  "type": "RuntimeError", "module": "", "message": "boom from flask",
  "frames": [ {"filename": "examples/flask_app.py", "abs_path": "/srv/app/examples/flask_app.py",
               "module": "examples.flask_app", "function": "boom", "lineno": 30, "colno": 0,
               "context": "        raise RuntimeError(\"boom from flask\") from exc",
               "pre_context": ["…"], "post_context": ["…"],
               "vars": {"exc": "KeyError('missing')"}, "in_app": true} ],
  "chain": [ {"type": "KeyError", "message": "'missing'", "frames": [], "is_cause": true, "relation": "cause"} ],
  "notes": []
}
```

- `frames` are **oldest first**: the crash site is the last frame (Python order).
- `in_app`: `true`/`false` from the SDK; omitted lets the server guess (site-packages, stdlib and
  framework paths are library frames).
- `chain[0]` is the direct cause (or context) of the top-level exception, `chain[1]` the cause of
  `chain[0]`, and so on. `relation` is `cause` (`raise … from`), `context` (implicit) or `group`
  (members of an `ExceptionGroup`).

## Grouping (fingerprint v2)

Type + module (unless `builtins`) + up to six in-app frames from the crash site outward, each as
`module-or-path:function` (no line numbers), + the chained cause types. No in-app frames → the
three innermost frames. No exception → `logger` + the message with numbers, hex, uuids and quoted
strings replaced. The culprit shown in lists is the innermost in-app frame.

## OpenTelemetry

`POST /v1/logs`, `/v1/traces` and `/v1/metrics` take OTLP/HTTP in protobuf or JSON. What each
one stores, and how OpenTelemetry fields map onto the ones above, is on the
[OpenTelemetry page](../send-data/opentelemetry.md).
