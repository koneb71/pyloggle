# Examples

Small, complete apps that send data to Pyloggle. Each one has a `/boom` page or a failing step,
so you can watch an error arrive. The same list is in the
[examples guide](https://koneb71.github.io/pyloggle/send-data/examples) on the documentation site.

## Before you start

Run a server (the [quick start](../README.md#quick-start) starts one with Docker), create an
access key under **Projects → Settings → New key**, then, from the repository root:

```bash
export PYLOGGLE_INGEST_URL=http://localhost:8080
export PYLOGGLE_API_KEY=pl_your_key_here
pip install -e ./python-sdk        # or: pip install pyloggle
```

The Python package examples also read `PYLOGGLE_ENV`, `PYLOGGLE_RELEASE` and
`PYLOGGLE_PROJECT_ID` when set.

## With the Python package

| Example | Run it | Then look at |
|---|---|---|
| [`script.py`](script.py): a plain script with a tag, a user, steps and an uncaught error | `python examples/script.py` | **Issues** for the error, **Logs** for its messages |
| [`flask_app.py`](flask_app.py): Flask, with every request counted | `pip install flask`, then `python examples/flask_app.py` and open <http://127.0.0.1:5050/hello> and `/boom` | The error's **Web request** tab, and the requests a minute later on **Analytics → Usage** |
| [`fastapi_app.py`](fastapi_app.py): FastAPI with the ASGI middleware, ready for FastAPI's own telemetry | `pip install fastapi uvicorn`, then `uvicorn examples.fastapi_app:app --port 8000` and open `/hello` and `/boom` | The error's **Web request** tab. With the OpenTelemetry settings below, **Requests** too |
| [`django_app.py`](django_app.py): a one-file Django site | `pip install django`, then `python examples/django_app.py runserver 8000` and `curl -H 'X-User: ada' localhost:8000/boom` | The SQL query among the **Steps before the error**, and one person affected |
| [`celery_app.py`](celery_app.py): Celery tasks, one of which fails | `pip install celery`, then `python examples/celery_app.py` (it runs its own worker in memory) | The task's issue, and `logger:pyloggle.celery` on **Logs** |

## With OpenTelemetry

These send through OpenTelemetry instead, and read its standard settings:

```bash
pip install -r examples/requirements-otel.txt
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080
export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=$PYLOGGLE_API_KEY"
export FASTAPI_OTEL_AUTO_CONFIGURE=true
export OTEL_SERVICE_NAME=otel-example
```

| Example | Run it | Then look at |
|---|---|---|
| [`otel_app.py`](otel_app.py): FastAPI's own OpenTelemetry (0.142 and later), without the Python package | `python -m uvicorn examples.otel_app:app --port 5052`, then open `/hello/ada` and `/boom` | **Requests** for the timelines, **Issues** for the error, **Dashboards** for the request metrics |
| [`otel_contrib_app.py`](otel_contrib_app.py): the OpenTelemetry contrib instrumentation, the way for any framework | `pip install opentelemetry-instrumentation-fastapi`, set `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`, then `python -m uvicorn examples.otel_contrib_app:app --port 5053` | **Requests** and **Logs** |
| [`otel-collector.yaml`](otel-collector.yaml): an OpenTelemetry Collector that forwards everything to Pyloggle | Change its `endpoint` to your server, run `PYLOGGLE_API_KEY=pl_... otelcol-contrib --config examples/otel-collector.yaml`, and point your apps at the Collector on port 4317 or 4318 | Whatever your apps send through it |

Each file's first lines repeat how to run it.

## Data without an app

To fill a server with two days of realistic errors, messages and requests, run
[`scripts/demo_data.py`](../scripts/demo_data.py). It needs nothing but Python:

```bash
python3 scripts/demo_data.py --url http://localhost:8080 --key "$PYLOGGLE_API_KEY"
```
