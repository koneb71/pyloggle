"""Celery integration: one scope per task run, failures captured once with task tags, request
ids carried from the publisher to the worker, retries as warnings, and workers that survive
prefork.

    from celery import Celery
    import pyloggle, pyloggle.celery

    app = Celery("shop", broker=...)
    pyloggle.init(service="shop-worker", env="prod")
    pyloggle.celery.init()                      # signals are global: once per process is enough

Every task run gets a scope with ``transaction`` = task name, the tags ``celery.task``,
``celery.task_id``, ``celery.queue`` and ``celery.retries``, and the request id of whoever
enqueued it (``apply_async`` from a web request carries the request's id and trace ids in the
message headers), so an error in a worker links back to the request that caused it.
"""
from __future__ import annotations

import threading
import time
import uuid
from typing import Any, Dict, Optional, Tuple

from . import scope as _scope
from .client import Client
from .context import ensure_request_id, parse_traceparent
from .stack import captured_event_id, exception_to_payload, mark_captured, safe_repr

_client: Optional[Client] = None
_emit_task_events = False
_capture_logs = True
_connected = False
_lock = threading.Lock()
_runs: Dict[str, Tuple[Any, float]] = {}  # task_id -> (scope token, start)

HEADER_REQUEST_ID = "pyloggle-request-id"
HEADER_TRACE_ID = "pyloggle-trace-id"
HEADER_SPAN_ID = "pyloggle-span-id"


def init(client: Optional[Client] = None, *, emit_task_events: bool = False, capture_logs: bool = True) -> Optional[Client]:
    """Connect the Celery signals to ``client`` (default: the global client). ``emit_task_events``
    logs one ``task`` line per run with its duration and state, like ``emit_request_latency``.
    ``capture_logs`` re-installs the logging handler after the worker's own logging setup
    (Celery replaces the root handlers unless ``worker_hijack_root_logger`` is off)."""
    global _client, _emit_task_events, _capture_logs, _connected
    if client is None:
        import pyloggle

        client = pyloggle.get_client()
    _client = client
    _emit_task_events = bool(emit_task_events)
    _capture_logs = bool(capture_logs)
    with _lock:
        if not _connected:
            _connect()
            _connected = True
    return client


def get_client() -> Optional[Client]:
    if _client is not None:
        return _client
    import pyloggle

    return pyloggle.get_client()


def _connect() -> None:
    from celery import signals

    signals.before_task_publish.connect(_before_publish, dispatch_uid="pyloggle.celery.publish", weak=False)
    signals.task_prerun.connect(_prerun, dispatch_uid="pyloggle.celery.prerun", weak=False)
    signals.task_postrun.connect(_postrun, dispatch_uid="pyloggle.celery.postrun", weak=False)
    signals.task_failure.connect(_failure, dispatch_uid="pyloggle.celery.failure", weak=False)
    signals.task_retry.connect(_retry, dispatch_uid="pyloggle.celery.retry", weak=False)
    signals.worker_process_init.connect(_worker_process_init, dispatch_uid="pyloggle.celery.fork", weak=False)
    signals.after_setup_logger.connect(_after_setup_logger, dispatch_uid="pyloggle.celery.logging", weak=False)


# --- publisher side ----------------------------------------------------------------------------


def _before_publish(sender: Any = None, headers: Any = None, body: Any = None, routing_key: Any = None, **_kw: Any) -> None:
    """Stamp the message with the publisher's request and trace ids; leave a breadcrumb."""
    try:
        if isinstance(headers, dict):
            sc = _scope.effective_scope()
            headers[HEADER_REQUEST_ID] = ensure_request_id()
            if sc.trace_id:
                headers[HEADER_TRACE_ID] = sc.trace_id
                if sc.span_id:
                    headers[HEADER_SPAN_ID] = sc.span_id
        name = str(sender or (headers or {}).get("task") or "")
        data: Dict[str, Any] = {}
        if routing_key:
            data["queue"] = str(routing_key)
        if isinstance(headers, dict) and headers.get("id"):
            data["task_id"] = str(headers["id"])
        _scope.add_breadcrumb(f"enqueued {name}".strip(), category="celery.publish", type="default", data=data)
    except Exception:
        return


# --- worker side ---------------------------------------------------------------------------------


def _header(req: Any, name: str) -> Optional[str]:
    """A custom message header: an attribute of task.request for messages from a broker, a key
    of task.request.headers for eager runs."""
    try:
        v = req.get(name)
    except Exception:
        v = getattr(req, name, None)
    if not v:
        hdrs = getattr(req, "headers", None)
        if isinstance(hdrs, dict):
            v = hdrs.get(name)
    return str(v) if v else None


def _queue_of(req: Any) -> str:
    try:
        info = getattr(req, "delivery_info", None) or {}
        return str(info.get("routing_key") or info.get("exchange") or "")
    except Exception:
        return ""


def _prerun(sender: Any = None, task_id: Any = None, task: Any = None, args: Any = None, kwargs: Any = None, **_kw: Any) -> None:
    client = get_client()
    if client is None:
        return
    try:
        token = _scope.push_scope(inherit=True)
        sc = _scope.effective_scope()
        req = getattr(task, "request", None)
        rid = _header(req, HEADER_REQUEST_ID) or (str(task_id) if task_id else None) or uuid.uuid4().hex
        sc.request_id = rid
        trace_id, span_id = parse_traceparent(_header(req, "traceparent"))
        if not trace_id:
            trace_id, span_id = _header(req, HEADER_TRACE_ID), _header(req, HEADER_SPAN_ID)
        if trace_id:
            sc.trace_id, sc.span_id = trace_id, span_id
        name = str(getattr(task, "name", None) or sender or "")
        sc.transaction = name
        sc.tags["celery.task"] = name
        if task_id:
            sc.tags["celery.task_id"] = str(task_id)
        queue = _queue_of(req)
        if queue:
            sc.tags["celery.queue"] = queue
        sc.tags["celery.retries"] = str(getattr(req, "retries", 0) or 0)
        _scope.add_breadcrumb(f"task {name}", category="celery.task", type="default", data={"task_id": str(task_id or "")})
        with _lock:
            _runs[str(task_id)] = (token, time.perf_counter())
    except Exception:
        return


def _failure(sender: Any = None, task_id: Any = None, exception: Any = None, args: Any = None, kwargs: Any = None, einfo: Any = None, **_kw: Any) -> None:
    client = get_client()
    if client is None or exception is None or captured_event_id(exception):
        return
    if bool(getattr(client.config, "otel_owns_exceptions", False)):
        mark_captured(exception, "otel")
        return
    try:
        task = sender
        name = str(getattr(task, "name", None) or "")
        req = getattr(task, "request", None)
        with _lock:
            run = _runs.get(str(task_id))
        duration_ms = round((time.perf_counter() - run[1]) * 1000, 2) if run else None
        celery_ctx: Dict[str, Any] = {"task": name, "task_id": str(task_id or ""), "retries": getattr(req, "retries", 0) or 0}
        queue = _queue_of(req)
        if queue:
            celery_ctx["queue"] = queue
        if args:
            celery_ctx["args"] = safe_repr(args, 512)
        if kwargs:
            celery_ctx["kwargs"] = safe_repr(kwargs, 512)
        ev: Dict[str, Any] = {
            "event_id": uuid.uuid4().hex,
            "ts": _scope.iso_now(),
            "level": "error",
            "logger": "pyloggle.celery",
            "message": str(exception) or type(exception).__name__,
            "exception": exception_to_payload(exception, config=client.config),
            "mechanism": {"type": "celery", "handled": False},
            "transaction": name or None,
            "extra": {"celery": celery_ctx},
            "attrs": {"celery.task": name},
        }
        if duration_ms is not None:
            ev["duration_ms"] = duration_ms
        mark_captured(exception, ev["event_id"])
        client.emit(ev)
    except Exception:
        return


def _retry(sender: Any = None, request: Any = None, reason: Any = None, einfo: Any = None, **_kw: Any) -> None:
    """A retry is a warning line, not an issue: the task runs again."""
    client = get_client()
    if client is None:
        return
    try:
        name = str(getattr(sender, "name", None) or "")
        why = getattr(reason, "exc", None) or reason
        client.emit(
            {
                "ts": _scope.iso_now(),
                "level": "warn",
                "logger": "pyloggle.celery",
                "message": f"task {name} retrying: {safe_repr(why, 200)}".strip(),
                "transaction": name or None,
                "attrs": {"celery.task": name, "celery.retries": str(getattr(request, "retries", 0) or 0), "celery.reason": safe_repr(why, 500)},
            }
        )
    except Exception:
        return


def _postrun(sender: Any = None, task_id: Any = None, task: Any = None, args: Any = None, kwargs: Any = None, retval: Any = None, state: Any = None, **_kw: Any) -> None:
    with _lock:
        run = _runs.pop(str(task_id), None)
    client = get_client()
    try:
        if client is not None and _emit_task_events and run is not None and str(state or "") != "FAILURE":
            name = str(getattr(task, "name", None) or sender or "")
            req = getattr(task, "request", None)
            attrs: Dict[str, Any] = {"celery.task": name, "celery.state": str(state or ""), "celery.retries": str(getattr(req, "retries", 0) or 0)}
            queue = _queue_of(req)
            if queue:
                attrs["celery.queue"] = queue
            client.emit(
                {
                    "ts": _scope.iso_now(),
                    "level": "info",
                    "logger": "pyloggle.celery",
                    "message": "task",
                    "transaction": name or None,
                    "duration_ms": round((time.perf_counter() - run[1]) * 1000, 2),
                    "attrs": attrs,
                }
            )
    except Exception:
        pass
    finally:
        if run is not None:
            _scope.reset_scope(run[0])


def _after_setup_logger(**_kw: Any) -> None:
    """The worker just (re)configured logging: put the pyloggle handler back so app and Celery
    log lines keep flowing and the task error line is still recognised as a duplicate."""
    client = get_client()
    if client is None or not _capture_logs:
        return
    try:
        from .logging_handler import install_pyloggle_logging

        install_pyloggle_logging(client)
    except Exception:
        return


def _worker_process_init(**_kw: Any) -> None:
    """Prefork child: make sure the sender thread exists in this process (the fork hook in
    Client already does this; calling it twice is harmless)."""
    client = get_client()
    if client is not None:
        try:
            client._after_fork()
        except Exception:
            pass
