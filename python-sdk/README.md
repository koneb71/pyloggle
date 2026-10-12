# pyloggle

The Python package for [Pyloggle](https://github.com/koneb71/pyloggle): self-hosted errors,
logs, request timelines and alerts. It sends your app's errors, log messages and requests to a
Pyloggle server. No dependencies, Python 3.9 and newer.

```bash
pip install pyloggle
```

## Start

Point it at your server with an access key that can send data:

```bash
export PYLOGGLE_INGEST_URL=https://logs.example.com
export PYLOGGLE_API_KEY=pl_your_key_here
```

Call `init()` once, as early as possible:

```python
import logging
import pyloggle

logging.basicConfig(level=logging.INFO)
pyloggle.init(service="checkout", env="production", release="2.3.0")
```

That one call sends every `logging` message, reports uncaught errors in the program and in
threads (and sends them before it exits), records the steps before each error (earlier
messages, and calls made with `requests`, `httpx` or `urllib3`), and adds the app, environment,
version and server to everything.

## Web apps and workers

```python
from pyloggle.flask import init_app                       # Flask
init_app(app, client)                                     # client = pyloggle.init(...)

from pyloggle.fastapi import PyloggleFastAPIMiddleware    # FastAPI, Starlette, any ASGI app
app.add_middleware(PyloggleFastAPIMiddleware, client=client)

MIDDLEWARE = ["pyloggle.django.PyloggleMiddleware", ...]  # Django, with PYLOGGLE = {...} in settings

import pyloggle.celery                                    # Celery, in the worker
pyloggle.celery.init()
```

Each request or task gets its own scope; an unhandled error is reported once, with the request's
details; `emit_request_latency=True` records every request with its duration.

## Report and add context

```python
pyloggle.capture_exception()                    # inside an except block
pyloggle.capture_message("stock sync skipped", level="warning")
pyloggle.set_user({"id": user.id})              # hashed before it leaves your app
pyloggle.set_tag("tenant", "acme")
log.info("order placed", extra={"order_id": 81723})   # searchable as @order_id:81723
```

Secrets are blanked out before sending (passwords, tokens, API keys, `Authorization` headers,
values that look like keys), and you can add your own field names with `scrub_keys`.

## Documentation

- [The Python package](https://koneb71.github.io/pyloggle/send-data/python): logging, errors,
  users and tags, grouping, flushing.
- [Frameworks](https://koneb71.github.io/pyloggle/send-data/frameworks): Flask, FastAPI, Django,
  Celery, scripts and asyncio.
- [Configuration](https://koneb71.github.io/pyloggle/send-data/configuration): every option.
- [OpenTelemetry](https://koneb71.github.io/pyloggle/send-data/opentelemetry): using it next to
  or instead of this package.
- [Running a Pyloggle server](https://koneb71.github.io/pyloggle/self-hosting/deployment).

## License

Apache License 2.0. See
[LICENSE](https://github.com/koneb71/pyloggle/blob/main/python-sdk/LICENSE).
