# Python package configuration

Every option can be passed to `pyloggle.init()`, put in Django's `PYLOGGLE` settings, or set on
a `ClientConfig`. A test in the repository checks that this page lists every option the package
has.

```python
pyloggle.init(
    service="checkout",
    env="production",
    release="2.3.0",
    scrub_keys=["iban"],
)
```

## Environment variables

These are read by `pyloggle.init()` when the matching argument is not given.

| Variable | Option |
|---|---|
| `PYLOGGLE_INGEST_URL` (or `PYLOGGLE_URL`) | `ingest_url` |
| `PYLOGGLE_API_KEY` (or `PYLOGGLE_TOKEN`) | `api_key` |
| `PYLOGGLE_SERVICE` | `service` |
| `PYLOGGLE_ENV` (or `PYLOGGLE_ENVIRONMENT`) | `env` |
| `PYLOGGLE_PROJECT_ID` | `project_id` |
| `PYLOGGLE_RELEASE` | `release` |
| `PYLOGGLE_DEBUG` (`1`, `true` or `yes`) | `debug` |

## Where to send

| Option | Default | What it does |
|---|---|---|
| `ingest_url` | `http://localhost:8080/v1/ingest` | The server's address. `https://logs.example.com` is enough; the package adds `/v1/ingest` |
| `api_key` | none | An access key with the *Send data* permission |
| `project_id` | `default` | Sent with each event; the server files events under the key's project |

## What to call your app

| Option | Default | What it does |
|---|---|---|
| `service` | `unknown` | The app's name, as Pyloggle shows it |
| `env` | `prod` | Where it runs, such as `production` or `staging` |
| `release` | none | Your app's version or git commit. `pyloggle.set_release()` changes it later |
| `hostname` | the machine's name | The server or container name to report |
| `tags` | `{}` | Tags added to every event, such as `{"region": "eu-west"}` |

## Web requests and tasks

| Option | Default | What it does |
|---|---|---|
| `count_requests` | `True` | Count every web request without listing it: one small tally per endpoint and minute, for **Analytics → Usage** and the request charts on **Dashboards**. See [Request counts](#request-counts) |
| `emit_request_latency` | `False` | Also list each web request on **Logs** as a `request` message, with its route, status, `duration_ms` and body. Counted requests need no message; turn this on to read or search requests one by one |
| `ignore_requests` | `("OPTIONS *", "HEAD *")` | Requests that are neither counted nor listed: `"METHOD pattern"` or just `"pattern"` for any method, matched against the path and the route, with `*` as a wildcard, such as `"/health"` or `"GET /static/*"`. The default skips CORS preflight and `HEAD` checks. Failed (5xx) requests and errors are always sent |
| `emit_request_body` | `True` | Include the request body in error reports and request messages. See [Request bodies](#request-bodies) |
| `max_request_body_bytes` | `4096` | Keep at most this much of a body, after secrets are blanked out |
| `capture_request_headers` | `True` | Include the request headers in error reports, with secrets blanked out |
| `max_request_headers` | `50` | Keep at most this many headers |
| `trust_incoming_request_id` | `True` | Use the `X-Request-ID` and `traceparent` headers sent by the caller |
| `trust_proxy_headers` | `False` | Take the client's IP address from `X-Forwarded-For`. Turn on only behind a proxy you control |

### Request counts

Every web request your app answers is counted, so **Analytics → Usage** can show how many
requests and how many people each page and endpoint gets, per version, and the request charts
on **Dashboards** fill in. Nothing appears on **Logs**: the package tallies requests in memory
per endpoint and minute, and sends each tally once its minute is over (or once it holds 1,000
requests) to `<ingest_url>/requests`. A tally holds the number of requests, how many failed,
each response time and the people who made them, as the same hash `set_user()` sends, never
their raw ID.

- A request that crashed is counted by its error report, and with OpenTelemetry the server
  counts each server span, so neither is counted twice.
- Requests that matched no route (a `404` for an unknown path) are not counted.
- With `emit_request_latency=True` each request is listed instead, and the server counts the
  listed requests.
- An older Pyloggle server answers `404`; the package then stops counting until the app
  restarts, and says so once in your app's log.

Turn counting off with `count_requests=False`.

### Request bodies

The body of each web request goes with the errors it causes, and with its `request` message
when `emit_request_latency=True`. Errors you log or capture yourself while handling a request
(`logger.exception(...)`, `pyloggle.capture_exception()`) carry it too. What is recorded
depends on the kind of body:

| The request sends | What is recorded |
|---|---|
| JSON | Its fields. Pyloggle shows them as a tree you can fold |
| A form (`application/x-www-form-urlencoded`) | Its fields, the same way |
| A form with files (`multipart/form-data`) | Its fields, and for each file only its name, type and size, such as `[file report.pdf, application/pdf, 12.3 KB]` |
| Text, XML, GraphQL | The text |
| Anything else (images, archives, binary data) | Only its type and size, such as `[image/png body, 1.2 MB, not recorded]` |

Fields whose name looks secret (`password`, `token`, `api_key` and the others on the
[security page](../self-hosting/security.md#what-the-python-package-leaves-out)) are blanked
out as `[REDACTED]` first. Then the body is cut to `max_request_body_bytes`, so a cut body
never shows a secret the whole body would have hidden. A body larger than 64 KB is not read
into memory just to be recorded. The package records what your app already read, or only its
type and size.

Turn bodies off with `emit_request_body=False`, or add field names to blank out with
`scrub_keys`. With OpenTelemetry, the body goes on the request's server span as the
`http.request.body` attribute instead (see [OpenTelemetry](opentelemetry.md)).

## People

| Option | Default | What it does |
|---|---|---|
| `user_id_salt` | none | A secret that turns the user ID hash into a keyed hash, so short IDs cannot be guessed back |
| `send_default_pii` | `False` | Also send the user's ID, e-mail address, user name and IP address as they are |

## Error reports

| Option | Default | What it does |
|---|---|---|
| `with_locals` | `True` | Include the values of local variables |
| `locals_scope` | `in_app` | Whose variables: `in_app` (your code), `all`, or `none` |
| `max_locals` | `20` | Variables per step |
| `max_local_repr` | `256` | Characters per variable value |
| `context_lines` | `5` | Lines of source before and after each line in the call chain |
| `max_frames` | `100` | Steps of the call chain to keep |
| `max_chain` | `5` | Chained exceptions (`raise … from …`) to follow |
| `in_app_include` | `()` | Module or path prefixes that are your code, when the package guesses wrong |
| `in_app_exclude` | `()` | Module or path prefixes that are not your code |
| `max_breadcrumbs` | `50` | Steps before the error to keep |
| `db_breadcrumbs` | `True` | Django: record each SQL statement, without its parameters, as a step |
| `http_breadcrumbs` | `True` | Record outgoing `requests`, `httpx` and `urllib3` calls as steps |
| `install_excepthooks` | `True` | Report uncaught errors in the program, threads and `asyncio` tasks |
| `otel_owns_exceptions` | `False` | Leave errors to OpenTelemetry: the integrations only mark them, so OpenTelemetry's report is the one kept. See [OpenTelemetry](opentelemetry.md#one-issue-per-crash) |

## Hiding secrets

| Option | Default | What it does |
|---|---|---|
| `scrub_keys` | `()` | More field names to blank out anywhere in an event, such as `["iban", "ssn"]` |
| `scrub` | the defaults | A `ScrubConfig` to replace the rules altogether: field names, name patterns, header and body names, and value patterns |

The default rules are listed on the
[security page](../self-hosting/security.md#what-the-python-package-leaves-out).

## Sending

| Option | Default | What it does |
|---|---|---|
| `batch_size` | `200` | Events per request to the server |
| `flush_interval_s` | `1.0` | Send at least this often, in seconds |
| `max_queue_size` | `10000` | Events waiting to be sent. When full, new events are dropped |
| `max_event_bytes` | `1000000` | Larger events lose variables, then steps, then the body, then the message, until they fit |
| `gzip` | `True` | Compress requests |
| `gzip_min_bytes` | `1024` | Only compress requests larger than this |
| `request_timeout_s` | `2.0` | Seconds to wait for the server |
| `max_retries` | `3` | Retries for a batch when the server is busy or unreachable |
| `retry_backoff_s` | `0.5` | First wait between retries, doubled each time |
| `shutdown_timeout_s` | `2.0` | Seconds to spend sending what is left when the program exits |
| `debug` | `False` | Log every batch that could not be sent, and why |

## Only for `pyloggle.init()`

| Argument | Default | What it does |
|---|---|---|
| `integrations` | `("logging", "excepthook", "http")` | What `init()` sets up: the logging handler, the hooks for uncaught errors, and the outgoing-call steps. Leave one out to skip it |
