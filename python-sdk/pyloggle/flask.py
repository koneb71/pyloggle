from __future__ import annotations

import time
from typing import Optional, Callable, Any

from .client import Client
from .context import ensure_request_id, set_request_id, set_user_id_hash, get_user_id_hash
from .stack import exception_to_payload
from .user import hash_user_id


def init_app(
    app,
    client: Client,
    *,
    user_id_getter: Optional[Callable[[], Optional[Any]]] = None,
) -> None:
    @app.before_request
    def _pyloggle_before_request():
        rid = ensure_request_id()
        set_request_id(rid)
        if user_id_getter is not None:
            try:
                uid = user_id_getter()
                set_user_id_hash(hash_user_id(uid, salt=client.config.user_id_salt))
            except Exception:
                pass

    @app.teardown_request
    def _pyloggle_teardown_request(exc: Optional[BaseException]):
        if exc is None:
            return
        try:
            from flask import request

            self_route = getattr(request, "url_rule", None)
            route = getattr(self_route, "rule", None) or request.path
            method = request.method
            ua = request.headers.get("User-Agent", "")
            remote = request.remote_addr or ""
            rid = ensure_request_id()
            user_id_hash = get_user_id_hash() or ""

            client.emit(
                {
                    "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                    "level": "error",
                    "logger": "pyloggle.flask",
                    "message": str(exc),
                    "request_id": rid,
                    "exception": exception_to_payload(exc),
                    "http": {
                        "method": method,
                        "route": route,
                        "status": 500,
                        "user_agent": ua,
                        "remote_addr": remote,
                    },
                    **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
                    "attrs": {},
                }
            )
        except Exception:
            return


