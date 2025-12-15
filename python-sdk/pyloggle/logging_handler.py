from __future__ import annotations

import logging
import time
from typing import Any, Dict, Optional

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


