"""Global exception hooks: uncaught exceptions in the main thread, in threads, in asyncio tasks
and unraisable errors are reported through the active client. Each hook chains to whatever was
installed before it, and installing again only re-points the hook at the newer client."""
from __future__ import annotations

import asyncio
import sys
import threading
from typing import Any, Optional

from .stack import captured_event_id, exception_to_payload, mark_captured

_client: Any = None
_installed: set = set()
_prev_excepthook = None
_prev_threading_hook = None
_prev_unraisable = None


def _event(exc: BaseException, mechanism: str, *, level: str = "fatal", message: Optional[str] = None) -> Optional[dict]:
    client = _client
    if client is None or exc is None:
        return None
    if captured_event_id(exc):
        return None
    import uuid

    from .scope import iso_now

    event_id = uuid.uuid4().hex
    ev = {
        "event_id": event_id,
        "ts": iso_now(),
        "level": level,
        "logger": "pyloggle." + mechanism,
        "message": message or (str(exc) or type(exc).__name__),
        "exception": exception_to_payload(exc, config=getattr(client, "config", None)),
        "mechanism": {"type": mechanism, "handled": False},
        "attrs": {},
    }
    mark_captured(exc, event_id)
    try:
        client.emit(ev)
    except Exception:
        return None
    return ev


def _flush(timeout: float = 2.0) -> None:
    client = _client
    if client is None:
        return
    try:
        client.flush(timeout)
    except Exception:
        pass


def _excepthook(etype, value, tb):
    try:
        if value is not None and value.__traceback__ is None:
            value = value.with_traceback(tb)
        if not isinstance(value, (KeyboardInterrupt, SystemExit)):
            _event(value, "excepthook")
            _flush()
    except Exception:
        pass
    prev = _prev_excepthook or sys.__excepthook__
    prev(etype, value, tb)


def _threading_hook(args):
    try:
        if args.exc_value is not None and not isinstance(args.exc_value, SystemExit):
            _event(args.exc_value, "threading")
    except Exception:
        pass
    prev = _prev_threading_hook
    if prev is not None:
        prev(args)


def _unraisable_hook(args):
    try:
        if args.exc_value is not None:
            _event(args.exc_value, "unraisablehook", level="error", message=str(args.err_msg or args.exc_value))
    except Exception:
        pass
    prev = _prev_unraisable
    if prev is not None:
        prev(args)


def install(client: Any) -> None:
    """Install sys/threading/unraisable hooks (idempotent; re-points at `client`)."""
    global _client, _prev_excepthook, _prev_threading_hook, _prev_unraisable
    _client = client
    if "sys" not in _installed:
        _prev_excepthook = sys.excepthook
        sys.excepthook = _excepthook
        _installed.add("sys")
    if "threading" not in _installed and hasattr(threading, "excepthook"):
        _prev_threading_hook = threading.excepthook
        threading.excepthook = _threading_hook  # type: ignore[assignment]
        _installed.add("threading")
    if "unraisable" not in _installed and hasattr(sys, "unraisablehook"):
        _prev_unraisable = sys.unraisablehook
        sys.unraisablehook = _unraisable_hook  # type: ignore[assignment]
        _installed.add("unraisable")


def uninstall() -> None:
    global _client
    if "sys" in _installed:
        sys.excepthook = _prev_excepthook or sys.__excepthook__
        _installed.discard("sys")
    if "threading" in _installed and _prev_threading_hook is not None:
        threading.excepthook = _prev_threading_hook  # type: ignore[assignment]
        _installed.discard("threading")
    if "unraisable" in _installed and _prev_unraisable is not None:
        sys.unraisablehook = _prev_unraisable  # type: ignore[assignment]
        _installed.discard("unraisable")
    _client = None


_ASYNCIO_FLAG = "_pyloggle_handler_installed"


def install_asyncio_hook(loop: Optional[asyncio.AbstractEventLoop] = None, client: Any = None) -> bool:
    """Report unhandled exceptions from asyncio tasks/callbacks on `loop` (default: running loop)."""
    global _client
    if client is not None:
        _client = client
    if loop is None:
        try:
            loop = asyncio.get_running_loop()
        except RuntimeError:
            return False
    if getattr(loop, _ASYNCIO_FLAG, False):
        return True
    prev = loop.get_exception_handler()

    def handler(lp, context):
        try:
            exc = context.get("exception")
            if isinstance(exc, BaseException) and not isinstance(exc, (SystemExit, KeyboardInterrupt)):
                _event(exc, "asyncio", level="error", message=str(context.get("message") or exc))
        except Exception:
            pass
        if prev is not None:
            prev(lp, context)
        else:
            lp.default_exception_handler(context)

    loop.set_exception_handler(handler)
    try:
        setattr(loop, _ASYNCIO_FLAG, True)
    except Exception:
        pass
    return True
