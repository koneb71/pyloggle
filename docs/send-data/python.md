# Python package

The `pyloggle` package sends your app's errors, log messages and requests to a Pyloggle
server. It has no dependencies of its own, works on Python 3.9 and newer, and never lets a
problem with Pyloggle break your app: if the server cannot be reached, events wait in a
bounded queue and are dropped when it fills up.

## Install

```bash
pip install pyloggle
```

Extras pull in a supported version of a framework, if your app does not already have one:
`pyloggle[flask]`, `pyloggle[fastapi]`, `pyloggle[django]`, `pyloggle[celery]` and
`pyloggle[otel]` (the OpenTelemetry API, to link events to traces).

## Start it

Tell the package where the server is and which access key to use, in your app's environment:

```bash
export PYLOGGLE_INGEST_URL=https://logs.example.com
export PYLOGGLE_API_KEY=pl_your_key_here
```

Then call `init()` once, as early as possible:

```python
import pyloggle

pyloggle.init(service="checkout", env="production", release="2.3.0")
```

That one call:

- **sends your logs**: every message your app writes with Python's `logging` becomes a message
  in Pyloggle, at the level your logging is set to;
- **reports uncaught errors** in the main program, in threads, and in objects being cleaned up,
  and sends them before the program exits;
- **records the steps before an error**: earlier log messages and the outgoing HTTP calls made
  with `requests`, `httpx` or `urllib3`;
- **adds context** to everything: app, environment, version, server, Python version, and the
  request and trace IDs when there is a request.

For web apps and workers, add the [framework integration](frameworks.md) too. It reports each
request and each failed task with its details.

`service` names the app, so you can tell your apps apart. `env` is where it runs, such as
`production` or `staging`. `release` is your app's version: Pyloggle shows which version an
error started in and flags a resolved problem that comes back in a newer one. A good release
is a version number or a git commit:

```python
pyloggle.init(service="checkout", env="production", release=os.environ.get("GIT_SHA"))
```

Every option is on the [configuration page](configuration.md).

## Log messages

Log as you always do. Anything you pass in `extra=` becomes a detail you can search on:

```python
log = logging.getLogger("checkout")
log.info("order placed", extra={"order_id": 81723, "total": 49.90})
```

In Pyloggle, find it with `attrs.order_id:81723` or `@order_id:81723`.

- `log.exception(...)`, or any call with `exc_info=True`, sends a full error report.
- Messages below `ERROR` are also kept as steps for the next error in the same request.
- Each message records the file, function and line it came from, as `code.filepath`,
  `code.function` and `code.lineno`.

Python's logging drops `info` and `debug` messages unless your app sets a lower level.
Pyloggle never changes your logging levels. Set one yourself, either before `pyloggle.init()`:

```python
logging.basicConfig(level=logging.INFO)   # does nothing once a handler is installed
pyloggle.init(service="checkout")
```

or at any time:

```python
logging.getLogger().setLevel(logging.INFO)
```

### Without `init()`

To add Pyloggle's handler to your own logging setup, for example in a `dictConfig`, create a
client and the handler yourself:

```python
import logging
from pyloggle import Client, ClientConfig
from pyloggle.logging_handler import PyloggleHandler, install_pyloggle_logging

client = Client(ClientConfig(ingest_url="https://logs.example.com", api_key="pl_...", service="checkout"))

logging.getLogger().addHandler(PyloggleHandler(client))   # just the root logger
# or: install_pyloggle_logging(client)                    # root plus web server loggers that do not propagate
```

## Report errors yourself

Uncaught errors are reported for you. For errors you catch and handle:

```python
try:
    charge(order)
except PaymentDeclined:
    pyloggle.capture_exception()                      # inside the except block
    show_message("Your card was declined.")
```

```python
pyloggle.capture_exception(exc, tags={"area": "billing"})   # an exception object
pyloggle.capture_message("stock sync skipped", level="warning")

@pyloggle.capture_errors()                            # report what a function raises, sync or async
def nightly_report():
    ...
```

Both `capture_*` functions return the event ID, which you can show to a user or put in a
support ticket.

**Grouping.** Pyloggle groups errors into issues by their type and the path through your code,
not by line numbers, so a fix that moves code keeps the same issue. To group differently, pass a
fingerprint: `fingerprint=["billing", "card-network-timeout"]`. `"{{ default }}"` in the list
stands for Pyloggle's own grouping, so `["{{ default }}", tenant]` splits an issue by tenant.

## Who, where and what

```python
pyloggle.set_user({"id": user.id})                    # counted, not named: see below
pyloggle.set_tag("tenant", "acme")                    # searchable, shown on issues
pyloggle.set_context("order", {"id": 81723, "items": 3})
pyloggle.add_breadcrumb("cache miss", category="cache", data={"key": "price:1042"})
```

`set_user` lets Pyloggle count **people affected**. The ID is hashed before it leaves your app,
and the e-mail address, user name and IP address are sent only with `send_default_pii=True`.
Set `user_id_salt` to a secret value so short IDs cannot be guessed back from their hash.

The framework integrations give each request and task its own scope, so tags and users set
during one request do not leak into the next. Elsewhere, isolate them yourself:

```python
with pyloggle.push_scope():
    pyloggle.set_tag("job", "import")
    run_import()
```

## What an error report contains

- the exception type and message, and each step of the call chain, with 5 lines of source
  around each line and whether it is your code or a library's;
- the values of local variables in your own code, with secrets blanked out;
- chained exceptions (`raise … from …` and errors raised while handling another), exception
  groups and notes;
- the steps before the error;
- the web request: method, route, address, headers with secrets blanked out, and the body if
  you turn it on;
- the user (as a hash), tags, contexts, app, environment, version, server and Python version.

The [security page](../self-hosting/security.md#what-the-python-package-leaves-out) lists
what is blanked out and how to add your own fields.

## Shutting down

Events are sent in the background, in batches. Uncaught errors and normal exits flush what is
left. In short-lived scripts or before `os._exit()`, flush yourself:

```python
pyloggle.flush()      # wait until everything queued is sent (up to 2 seconds)
pyloggle.close()      # flush and stop the background sender
```

Processes that fork, such as gunicorn with `--preload` or Celery's prefork workers, get a fresh
sender in each child automatically.

## When nothing arrives

Set `PYLOGGLE_DEBUG=1` and run the app again: the package then logs why it could not send, for
example a refused key or an unreachable server. Then check:

- `PYLOGGLE_INGEST_URL` is the server's address; the package adds `/v1/ingest` itself.
- The key has the *Send data* permission and belongs to the project you are looking at.
- Python's logging level lets `info` messages through, if those are what you are missing.
- The server's **System** page, or `GET /healthz?verbose=1`, shows whether events were
  dropped on the server, for example by a rate limit.
