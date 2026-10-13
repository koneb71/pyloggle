# Frameworks

Each integration gives every request or task its own scope, reports an unhandled error exactly
once with the request's details, and can record every request with its duration. Call
`pyloggle.init()` first, as on the [Python package](python.md) page; the integrations use
that client.

| Your app | Add |
|---|---|
| [Flask](#flask) | `init_app(app, client)` |
| [FastAPI, Starlette, any ASGI app](#fastapi-and-asgi) | `app.add_middleware(PyloggleFastAPIMiddleware, client=client)` |
| [Django](#django) | One middleware and a settings dictionary |
| [Celery](#celery) | `pyloggle.celery.init()` in the worker |
| [Scripts, cron jobs, threads](#scripts-and-threads) | Nothing beyond `init()` |

## What every integration records

- **An ID for the request**, taken from an incoming `X-Request-ID` header or made up, and the
  trace ID from an incoming W3C `traceparent` header. Every message the request logs carries
  both, so **Logs** can show all messages of one request.
- **The request** on errors: method, route pattern (`/orders/{id}`, not `/orders/81723`),
  full address, query string, headers and body with secrets blanked out, and client IP. Errors
  you log yourself while handling the request carry the same. See
  [Request bodies](configuration.md#request-bodies) for what a body becomes.
- **One error per crash.** The framework usually logs the crash too; that line is kept but
  marked as the same error, so it does not become a second issue.
- **Each request**, with `emit_request_latency=True`: one `request` message per request with its
  route, status, `duration_ms` and body. These fill the response-time charts on **Dashboards** and let
  you search for slow requests with `duration_ms:>1000`.

## Flask

```bash
pip install "pyloggle[flask]"
```

```python
from flask import Flask
import pyloggle
from pyloggle.flask import init_app

client = pyloggle.init(service="shop", env="production", emit_request_latency=True)
app = Flask(__name__)
init_app(app, client)
```

To count the people affected, tell it how to find the signed-in user:

```python
from flask_login import current_user

init_app(app, client, user_id_getter=lambda: current_user.get_id())
```

## FastAPI and ASGI

```bash
pip install "pyloggle[fastapi]"
```

```python
from fastapi import FastAPI
import pyloggle
from pyloggle.fastapi import PyloggleFastAPIMiddleware

client = pyloggle.init(service="api", env="production", emit_request_latency=True)
app = FastAPI()
app.add_middleware(PyloggleFastAPIMiddleware, client=client)
```

The same middleware works for Starlette, Litestar and any other ASGI app (it is also available
as `pyloggle.asgi.PyloggleASGIMiddleware`). It also reports errors in background tasks started
with `asyncio`. `user_id_getter` receives the ASGI scope:

```python
app.add_middleware(PyloggleFastAPIMiddleware, client=client, user_id_getter=lambda scope: scope.get("user_id"))
```

FastAPI 0.142 and later can also send its own request timelines and metrics through
OpenTelemetry. Pyloggle combines both into the best of each, as the
[OpenTelemetry page](opentelemetry.md#native-telemetry-in-fastapi) shows:

```python
from pyloggle.fastapi import PyloggleFastAPIMiddleware, telemetry

app = FastAPI(telemetry=telemetry(exclude=lambda scope: scope["path"] == "/health"))
app.add_middleware(PyloggleFastAPIMiddleware, client=client)
```

## Django

```bash
pip install "pyloggle[django]"
```

In `settings.py`, add the middleware first, so it sees every error, and the settings:

```python
MIDDLEWARE = [
    "pyloggle.django.PyloggleMiddleware",
    # ... your other middleware
]

PYLOGGLE = {
    "service": "shop",
    "env": "production",
    "emit_request_latency": True,
}
```

`PYLOGGLE` takes any [option](configuration.md) of `pyloggle.init()`. The server address and
the key can go in it too, but are better kept out of your code, in `PYLOGGLE_INGEST_URL` and
`PYLOGGLE_API_KEY`. The client starts on the first request. To start it earlier, for example to catch errors in management commands, call
`pyloggle.django.init()` from your app's `AppConfig.ready()`.

On top of the common records, Django gives:

- the signed-in user from `request.user`, hashed;
- one step per SQL query before an error, with the statement but never its parameters
  (`db_breadcrumbs=False` turns this off);
- both sync and async views.

## Celery

```bash
pip install "pyloggle[celery]"
```

Where the worker starts, after `pyloggle.init()`:

```python
import pyloggle
import pyloggle.celery

pyloggle.init(service="worker", env="production")
pyloggle.celery.init(emit_task_events=True)
```

- A task that fails becomes one issue with the task's name, arguments (secrets blanked out) and
  the ID of the request that queued it, so you can follow a web request into its tasks.
- Retries are logged as warnings.
- `emit_task_events=True` logs one `task` message per run with its state and `duration_ms`.
- Each task run has its own scope, tagged with `celery.task`, `celery.task_id`, `celery.queue`
  and `celery.retries`.
- Prefork workers get a fresh sender in each child process.

Celery replaces the logging setup of the worker when it starts; `pyloggle.celery.init()` adds
Pyloggle's handler back afterwards.

## Scripts and threads

`pyloggle.init()` alone covers scripts, cron jobs and workers without a framework: uncaught
errors in the main program and in threads are reported and sent before the program exits, and
logging works as usual.

For `asyncio` programs, report errors in tasks nobody waits for:

```python
async def main():
    pyloggle.install_asyncio_hook()
    ...
```

To report what one function raises, without catching it:

```python
@pyloggle.capture_errors()
def sync_stock():
    ...
```

## Outgoing calls

`pyloggle.init()` also records each call your app makes with `requests`, `httpx` or `urllib3` as
a step: method, address without the query string or credentials, status and duration. Failed
calls are marked as failed. Set `http_breadcrumbs=False` to turn this off.
