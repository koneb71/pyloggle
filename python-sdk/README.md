# pyloggle (Python SDK)

Logging + error tracking client for a self-hosted [Pyloggle](../README.md) server. No dependencies.

```bash
pip install pyloggle            # or: pip install -e ./python-sdk
pip install "pyloggle[flask]"   # optional framework extras: flask, fastapi
```

## 30-second start

```python
import pyloggle

pyloggle.init(service="checkout", env="prod")   # PYLOGGLE_INGEST_URL / PYLOGGLE_API_KEY from the environment
```

That single call:

- installs a stdlib `logging` handler, so every `logging` call becomes a log line (with the structured
  traceback when `exc_info` is present);
- installs `sys.excepthook`, `threading.excepthook` and `sys.unraisablehook`, so uncaught exceptions in
  scripts, threads and finalizers become issues (and are flushed before the interpreter exits);
- enriches every event with service, env, release, host, runtime, SDK version, request/trace ids,
  tags, user and the breadcrumb trail.

Environment variables: `PYLOGGLE_INGEST_URL`, `PYLOGGLE_API_KEY` (or `PYLOGGLE_TOKEN`), `PYLOGGLE_PROJECT_ID`,
`PYLOGGLE_SERVICE`, `PYLOGGLE_ENV`, `PYLOGGLE_RELEASE`, `PYLOGGLE_DEBUG`.

## Capture API

```python
pyloggle.capture_exception()                                  # inside an except block
pyloggle.capture_exception(exc, tags={"area": "billing"}, fingerprint=["billing", "timeout"])
pyloggle.capture_message("cache rebuilt", level="info")
pyloggle.add_breadcrumb("cache miss", category="cache", data={"key": "k1"})
pyloggle.set_user({"id": "42", "email": "a@b.c"})           # id is hashed; raw fields need send_default_pii=True
pyloggle.set_tag("tenant", "acme"); pyloggle.set_context("order", {"id": 7})

with pyloggle.push_scope():                                   # isolate tags/user/breadcrumbs
    ...

@pyloggle.capture_errors()                                    # sync or async
def job(): ...

pyloggle.install_asyncio_hook()                               # inside a running loop: unhandled task errors
pyloggle.flush(); pyloggle.close()
```

## Frameworks

```python
from pyloggle.flask import init_app
init_app(app, client)                                         # client = pyloggle.init(...)

from pyloggle.fastapi import PyloggleFastAPIMiddleware        # also pyloggle.asgi for any ASGI app
app.add_middleware(PyloggleFastAPIMiddleware, client=client)
```

Each request gets its own scope: request id (honors `X-Request-ID`), W3C `traceparent` trace/span ids,
route, URL, query, scrubbed headers, client IP, user; one error event per unhandled exception (the
framework's own error log line is kept but marked as a duplicate, so no second issue appears).

## What an error report contains

Exception type/module/message, frames oldest→newest with source context (5 lines before/after),
in-app flag, local variables for in-app frames (secrets redacted by name and by value pattern),
chained causes (`raise ... from`, implicit context), exception groups, `__notes__`, breadcrumbs,
request (method, route, URL, query, headers, body when enabled), user, runtime, SDK, tags, extra.

## Configuration (`ClientConfig` / `init(**kwargs)`)

| Option | Default | Meaning |
|---|---|---|
| `emit_request_latency` | `False` | one `request` log per request with `latency_ms` |
| `emit_request_body` / `max_request_body_bytes` | `False` / 4096 | capture request bodies on error events |
| `capture_request_headers` / `max_request_headers` | `True` / 50 | scrubbed headers on error events |
| `trust_incoming_request_id` / `trust_proxy_headers` | `True` / `False` | honor `X-Request-ID`/`traceparent`; take client ip from `X-Forwarded-For` |
| `send_default_pii` | `False` | send raw user id/email/username/ip |
| `with_locals` / `locals_scope` | `True` / `in_app` | local variables on frames (`all`, `in_app`, `none`) |
| `context_lines` / `max_frames` / `max_chain` | 5 / 100 / 5 | report size limits |
| `in_app_include` / `in_app_exclude` | `()` | module or path prefixes that are (not) your code |
| `max_breadcrumbs` | 50 | breadcrumbs attached to error events |
| `install_excepthooks` | `True` | global hooks (also set by `init(integrations=...)`) |
| `gzip`, `max_retries`, `retry_backoff_s`, `request_timeout_s` | `True`, 3, 0.5, 2.0 | transport |
| `max_event_bytes` | 1 MB | oversize events shed locals → breadcrumbs → body → message |
| `scrub` / `scrub_keys` | defaults | redaction rules (keys, header/body keys, value patterns) |

## Upgrading from 0.1

Nothing to change: `Client(ClientConfig(...))`, `init_app`, `PyloggleFastAPIMiddleware`,
`PyloggleHandler`, `install_pyloggle_logging` keep their signatures. New behavior you get for free:
timestamps are now RFC 3339 (0.1 handler events were silently rejected by the server), uncaught
exceptions are reported, reports carry the full context above.

## Development

```bash
python -m venv .venv && . .venv/bin/activate
pip install -e '.[test]'
pytest -q
python tests/gen_golden.py   # regenerate the wire fixture consumed by the Go decoder tests
```
