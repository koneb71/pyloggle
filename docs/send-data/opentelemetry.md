# OpenTelemetry

Pyloggle speaks OTLP/HTTP. Anything that exports OpenTelemetry can send logs, traces and
metrics to it with one API key, with or without the Python SDK.

| Door | Payload | Lands in |
|---|---|---|
| `POST /v1/logs` | log records | log lines; records with `exception.*` attributes become issues (Python tracebacks in `exception.stacktrace` are parsed back into frames) |
| `POST /v1/traces` | spans | the `spans` table (7 days) for the **Requests** page; server spans also produce a `request` log line; failures become issues (rules below) |
| `POST /v1/metrics` | gauges, sums, histograms | `metrics_points` (30 days) for the **Custom measurements** section of Dashboards and its web request numbers |

Protobuf (`application/x-protobuf`) and JSON encodings are accepted, gzip too, also under
`/v1/otlp/v1/*`. Authenticate with `X-API-Key: <key>` or `Authorization: Bearer <key>`; the key
needs the *Send data* permission (every key created on the Projects page with *Send data*
ticked, and every key migrated from 0.2).

## Exporter settings

Python, any framework, with the OpenTelemetry SDK:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=https://pyloggle.example.com     # the signal paths are appended
export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=pl_..."
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_SERVICE_NAME=checkout
export OTEL_RESOURCE_ATTRIBUTES=deployment.environment=prod,service.version=1.4.0
```

`service.name`, `deployment.environment` (or `deployment.environment.name`) and
`service.version` map to pyloggle's service, env and release.

Through the OpenTelemetry Collector, use the `otlphttp` exporter;
[`examples/otel-collector.yaml`](https://github.com/koneb71/pyloggle/blob/main/examples/otel-collector.yaml) is a complete pipeline for logs, traces and metrics:

```yaml
exporters:
  otlphttp/pyloggle:
    endpoint: https://pyloggle.example.com
    headers:
      x-api-key: ${env:PYLOGGLE_API_KEY}
```

Django and Celery apps that prefer the contrib instrumentations run unchanged under
`opentelemetry-instrument` with the same environment:

```bash
pip install opentelemetry-distro opentelemetry-exporter-otlp-proto-http
opentelemetry-bootstrap -a install          # picks django, celery, requests, psycopg, ...
opentelemetry-instrument gunicorn shop.wsgi
opentelemetry-instrument celery -A shop worker
```

Exceptions the instrumentations record on spans become issues; the SDK can run alongside
(see "One issue per crash").

## Native telemetry in FastAPI

FastAPI 0.142 and later ships its own OpenTelemetry support: one `SERVER` span per request plus operation spans
(`fastapi.dependencies`, `fastapi.endpoint`, `fastapi.serialization`, background tasks), the
HTTP metrics `http.server.request.duration` and `http.server.active_requests`, and one log
record per unhandled exception and per validation failure. No contrib package, no SDK:

```bash
pip install "fastapi>=0.143" opentelemetry-sdk opentelemetry-exporter-otlp-proto-http
export OTEL_EXPORTER_OTLP_ENDPOINT=https://pyloggle.example.com
export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=pl_..."
export FASTAPI_OTEL_AUTO_CONFIGURE=true        # 0.143+: exporters from the environment are opt-in
export OTEL_SERVICE_NAME=checkout
```

[`examples/otel_app.py`](https://github.com/koneb71/pyloggle/blob/main/examples/otel_app.py) is the runnable version. What you get in pyloggle:

- **Requests** lists `GET /boom`; its timeline shows the operation steps, the messages logged
  during the request and the issue it caused.
- The exception log (`http.server.request.exception`, body "Unhandled exception in FastAPI
  request") becomes **one issue** with parsed frames; the failed request and endpoint spans
  do not become issues of their own.
- The **Web requests** section of Dashboards can read FastAPI's HTTP metrics (choose *Based on:
  OpenTelemetry metrics*, or leave it on *Automatic*): requests per second and the failed share
  from the duration histogram, and typical and slow response times by `http.route`.
- Validation failures (`fastapi.validation.failed`) land in the explorer as warnings.

`FastAPI(telemetry={...})` options that matter: `exclude` (a function of the ASGI scope; use it
for health checks), `operation_spans` (off to keep only the request span), `logs` (off when
something else reports exceptions), `tracing` / `metrics`. FastAPI 0.143 and later only add
exporters from the environment when `FASTAPI_OTEL_AUTO_CONFIGURE=true`; 0.142 did so on its own.

### SDK + native telemetry

The richest setup: FastAPI's spans and metrics, the SDK's error reports (locals, breadcrumbs,
request body, user). `pyloggle.fastapi.telemetry()` returns the telemetry dict with
`logs=False`, so the SDK owns exceptions:

```python
from fastapi import FastAPI
import pyloggle
from pyloggle.fastapi import PyloggleFastAPIMiddleware, telemetry

client = pyloggle.init(service="checkout", env="prod", emit_request_latency=True)
app = FastAPI(telemetry=telemetry(exclude=lambda s: s["path"] == "/health"))
app.add_middleware(PyloggleFastAPIMiddleware, client=client)
```

The native span wraps the whole middleware stack, so the SDK middleware runs inside it: the
occurrence carries the native trace and span ids (the trace view links both ways), the span is
stamped with `pyloggle.event_id`, and the `request` line comes from the span rather than from
the SDK. Leaving `logs=True` is safe as well: the server folds FastAPI's exception log and the
SDK's report of the same crash into one occurrence (below).

## What is stored, what is dropped

- **Spans**: every kind, with attributes and events (events capped at 64 KiB per span),
  partitioned by day, 7 days by default (`PYLOGGLE_RETENTION_SPANS_DAYS`). `http.route` is the
  route; `url.path` / `http.target` stay attributes and are never used as a route (request
  stats would explode on raw paths). `OTLP_STORE_SPANS=false` keeps only the logs and issues
  derived from spans; `OTLP_STORE_SERVER_SPANS=false` stops the `request` lines.
- **Metrics**: gauges and sums keep their value; histograms keep count, sum, bounds and bucket
  counts; summaries and exponential histograms keep count and sum only. At most 16 attribute
  keys per point; the series hash is over the sorted attributes. 30 days by default
  (`PYLOGGLE_RETENTION_METRICS_DAYS`); `OTLP_STORE_METRICS=false` discards them. Cumulative
  series are differenced per series with reset detection, against the last sample before the
  window, so short-lived processes count in full.
- **Logs**: body, severity, attributes, resource and scope; `trace_id` / `span_id` link them
  to spans. `exception.type`, `exception.message` and `exception.stacktrace` make an issue.
- **Issues from spans**: a span event named `exception` is an issue occurrence. A span with
  `status = ERROR` and no exception event: when `error.type` names an exception class the
  request span yields a failed-request line without an issue (the exception itself arrives
  as a log, a span event or the SDK's report) and failed child spans yield nothing; when
  `error.type` is an HTTP status (`503`) or absent, the failure is an issue titled
  `<span name> failed: <status message>`, for child spans too (an upstream call that failed).
- Not supported this round: OTLP/gRPC, span links, exemplars, exponential-histogram buckets,
  tail sampling, a service map.

## One issue per crash

A crash can reach pyloggle twice: through the SDK (`/v1/ingest`) and through OpenTelemetry (an
exception log or a span event). The server remembers the first error occurrence per project,
trace id and exception class for two minutes; a second report in that window is stored as a
log line marked `skip_issue`, pointing at the first event and its issue, whichever arrived
first. A retried OTLP batch is folded the same way. The SDK also stamps the active span with
`pyloggle.event_id`, and a stamped span never produces an error or request line of its own.

For teams that prefer the OTLP path, `pyloggle.init(otel_owns_exceptions=True)` makes every
framework integration skip exception capture and only mark the exception, so the framework's
own error log line stays a duplicate.
