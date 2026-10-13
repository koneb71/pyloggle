# Examples

The [`examples/`](https://github.com/koneb71/pyloggle/tree/main/examples) folder has small,
complete apps that send data to Pyloggle. Each one has a `/boom` page or a failing step, so
you can see an error arrive.

Run them from the repository root, with a server running (the [quick start](../introduction/quick-start.md)
starts one) and these set:

```bash
export PYLOGGLE_INGEST_URL=http://localhost:8080
export PYLOGGLE_API_KEY=pl_your_key_here
pip install -e ./python-sdk        # or: pip install pyloggle
```

| Example | Run it | Then look at |
|---|---|---|
| [`script.py`](https://github.com/koneb71/pyloggle/blob/main/examples/script.py): a plain script with tags, a user and an uncaught error | `python examples/script.py` | **Issues** for the error, **Logs** for its messages |
| [`flask_app.py`](https://github.com/koneb71/pyloggle/blob/main/examples/flask_app.py): Flask with request timing | `pip install flask` then `python examples/flask_app.py`, and open `http://127.0.0.1:5050/hello` and `/boom` | The error's **Web request** tab; `logger:pyloggle.flask` on **Logs** for each request |
| [`fastapi_app.py`](https://github.com/koneb71/pyloggle/blob/main/examples/fastapi_app.py): FastAPI with the ASGI middleware, ready for FastAPI's own telemetry | `pip install fastapi uvicorn` then `uvicorn examples.fastapi_app:app --port 8000`, and open `/hello` and `/boom` | The error's **Web request** tab; add the OpenTelemetry settings below for **Requests** |
| [`django_app.py`](https://github.com/koneb71/pyloggle/blob/main/examples/django_app.py): a one-file Django site | `pip install django` then `python examples/django_app.py runserver 8000` | The SQL queries among the **Steps before the error** |
| [`celery_app.py`](https://github.com/koneb71/pyloggle/blob/main/examples/celery_app.py): Celery tasks, one of which fails and retries | `pip install celery` then `python examples/celery_app.py` (it runs its own worker in memory) | The task's issue, and `logger:pyloggle.celery` on **Logs** |
| [`otel_app.py`](https://github.com/koneb71/pyloggle/blob/main/examples/otel_app.py): FastAPI's own OpenTelemetry, without the Python package | See its first lines: it needs the OpenTelemetry settings below | **Requests** for the timelines, **Dashboards** for the request metrics |
| [`otel_contrib_app.py`](https://github.com/koneb71/pyloggle/blob/main/examples/otel_contrib_app.py): the OpenTelemetry contrib instrumentation, for any framework | See its first lines | **Requests** and **Logs** |
| [`otel-collector.yaml`](https://github.com/koneb71/pyloggle/blob/main/examples/otel-collector.yaml): an OpenTelemetry Collector that forwards everything to Pyloggle | `PYLOGGLE_API_KEY=pl_... otelcol-contrib --config examples/otel-collector.yaml` | Whatever your apps send through the Collector |

The OpenTelemetry examples read the standard settings:

```bash
pip install -r examples/requirements-otel.txt
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080
export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=$PYLOGGLE_API_KEY"
export FASTAPI_OTEL_AUTO_CONFIGURE=true
export OTEL_SERVICE_NAME=otel-example
```

To fill a server with two days of realistic data instead, without running any app, use
[`scripts/demo_data.py`](https://github.com/koneb71/pyloggle/blob/main/scripts/demo_data.py):

```bash
python3 scripts/demo_data.py --url http://localhost:8080 --key "$PYLOGGLE_API_KEY"
```
