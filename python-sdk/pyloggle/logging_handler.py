from __future__ import annotations

import logging
import time
from typing import Any, Dict, Optional, Sequence

from .client import Client
from .context import get_request_id, get_trace_id, ensure_request_id, get_user_id_hash
from .stack import exception_to_payload


class PyloggleHandler(logging.Handler):
    def __init__(self, client: Client):
        super().__init__()
        self.client = client

    def emit(self, record: logging.LogRecord) -> None:
        try:
            ensure_request_id()
            ev: Dict[str, Any] = {
                "ts": time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(record.created)),
                "level": record.levelname.lower(),
                "logger": record.name,
                "message": record.getMessage(),
                "request_id": get_request_id() or "",
                "trace_id": get_trace_id() or "",
                "attrs": {},
            }

            user_id_hash = get_user_id_hash() or ""
            if user_id_hash:
                ev["user"] = {"id_hash": user_id_hash}

            if record.exc_info and record.exc_info[1]:
                ev["exception"] = exception_to_payload(record.exc_info[1])
                ev["level"] = "error"

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

