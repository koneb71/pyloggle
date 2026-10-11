from __future__ import annotations

import logging
import time
import uuid
from typing import Any, Dict, Optional, Sequence

from . import scope as _scope
from .client import Client, normalize_level
from .context import ensure_request_id, get_request_id, get_trace_id, get_user_id_hash
from .stack import captured_event_id, exception_to_payload, mark_captured

# LogRecord attributes that are not user-supplied `extra=` fields.
_STD_RECORD_ATTRS = frozenset(
    {
        "name", "msg", "args", "levelname", "levelno", "pathname", "filename", "module", "exc_info", "exc_text",
        "stack_info", "lineno", "funcName", "created", "msecs", "relativeCreated", "thread", "threadName",
        "processName", "process", "message", "asctime", "taskName",
    }
)


def _iso(created: float) -> str:
    return _scope._iso(created)


class PyloggleHandler(logging.Handler):
    """stdlib logging handler: every record becomes a log event; records with exc_info become
    error events with the structured traceback, unless the exception was already reported by an
    integration (then the line is kept but flagged as a duplicate so no second issue is created).
    Records below ERROR are also kept as breadcrumbs for later error events."""

    def __init__(self, client: Client):
        super().__init__()
        self.client = client

    def emit(self, record: logging.LogRecord) -> None:
        try:
            cfg = getattr(self.client, "config", None)
            ensure_request_id()
            level = normalize_level(record.levelname)
            attrs: Dict[str, Any] = {}
            for k, v in record.__dict__.items():
                if k in _STD_RECORD_ATTRS or k.startswith("_"):
                    continue
                attrs[k] = v
            if record.pathname:
                attrs["code.filepath"] = record.pathname
            if record.funcName:
                attrs["code.function"] = record.funcName
            if record.lineno:
                attrs["code.lineno"] = record.lineno
            if record.stack_info:
                attrs["stack_info"] = record.stack_info

            ev: Dict[str, Any] = {
                "ts": _iso(record.created),
                "level": level,
                "logger": record.name,
                "message": record.getMessage(),
                "request_id": get_request_id() or "",
                "trace_id": get_trace_id() or "",
                "attrs": attrs,
            }

            user_id_hash = get_user_id_hash() or ""
            if user_id_hash:
                ev["user"] = {"id_hash": user_id_hash}

            exc = record.exc_info[1] if record.exc_info and len(record.exc_info) > 1 else None
            if exc is not None:
                prior = captured_event_id(exc)
                if prior:
                    ev["skip_issue"] = True
                    attrs["pyloggle.duplicate_of"] = prior
                else:
                    ev["event_id"] = uuid.uuid4().hex
                    ev["exception"] = exception_to_payload(exc, config=cfg)
                    ev["mechanism"] = {"type": "logging", "handled": True}
                    if level not in ("error", "fatal"):
                        ev["level"] = "error"
                    mark_captured(exc, ev["event_id"])
            elif record.levelno < logging.ERROR and int(getattr(cfg, "max_breadcrumbs", 50) or 0) > 0:
                _scope.add_breadcrumb(
                    record.getMessage(), category=record.name, level=level, type="log", ts=record.created
                )

            self.client.emit(ev)
        except Exception:
            return


DEFAULT_FRAMEWORK_LOGGER_NAMES: tuple[str, ...] = (
    # Flask / Werkzeug
    "werkzeug",
    # Gunicorn
    "gunicorn.error",
    "gunicorn.access",
    # Uvicorn / FastAPI (ASGI)
    "uvicorn",
    "uvicorn.error",
    "uvicorn.access",
)


def _has_pyloggle_handler(logger: logging.Logger) -> bool:
    for h in logger.handlers:
        if isinstance(h, PyloggleHandler):
            return True
    return False


def _first_pyloggle_handler(logger: logging.Logger) -> Optional[PyloggleHandler]:
    for h in logger.handlers:
        if isinstance(h, PyloggleHandler):
            return h
    return None


def _ensure_pyloggle_handler(logger: logging.Logger, handler: PyloggleHandler) -> None:
    # Idempotent: do not add duplicates if called multiple times (e.g. init_app twice).
    if _has_pyloggle_handler(logger):
        return
    logger.addHandler(handler)


def install_pyloggle_logging(
    client: Client,
    *,
    capture_root_logger: bool = True,
    capture_framework_loggers: bool = True,
    framework_logger_names: Optional[Sequence[str]] = None,
) -> None:
    """
    Best-effort installation of Pyloggle's stdlib logging handler.

    - Adds a `PyloggleHandler` to the root logger (captures most app logs).
    - Also ensures common framework/access loggers are captured when they do NOT propagate
      to root (e.g. when a server sets `propagate = False`).

    This function is idempotent and does NOT modify logger levels, propagation, or existing
    handlers; it only adds Pyloggle's handler when missing.
    """
    root = logging.getLogger()
    root_has = _has_pyloggle_handler(root)

    if capture_root_logger:
        if not root_has:
            _ensure_pyloggle_handler(root, PyloggleHandler(client))
            root_has = True

    if not capture_framework_loggers:
        return

    names = tuple(framework_logger_names) if framework_logger_names is not None else DEFAULT_FRAMEWORK_LOGGER_NAMES
    for name in names:
        try:
            lg = logging.getLogger(name)
            # Avoid duplicates: if root is capturing and this logger propagates, root will see it.
            if root_has and bool(getattr(lg, "propagate", True)):
                continue

            # Reuse any existing pyloggle handler from root if present; otherwise create one.
            handler = _first_pyloggle_handler(root) or PyloggleHandler(client)
            _ensure_pyloggle_handler(lg, handler)
        except Exception:
            continue
