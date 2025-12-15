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
    capture_logs: bool = True,
) -> None:
    if capture_logs:
        try:
            from .logging_handler import install_pyloggle_logging

            install_pyloggle_logging(client)
        except Exception:
            pass

    # If Flask is present, mark whether an exception occurred during the request so we can
    # avoid emitting a "successful request" timing event for handled exceptions.
    try:
        from flask import got_request_exception  # type: ignore

        def _pyloggle_mark_exception(_sender, _exception, **_extra):
            try:
                from flask import g

                g._pyloggle_had_exception = True
            except Exception:
                return

        got_request_exception.connect(_pyloggle_mark_exception, app)
    except Exception:
        pass

    @app.before_request
    def _pyloggle_before_request():
        try:
            from flask import g

            g._pyloggle_request_start = time.perf_counter()
            g._pyloggle_had_exception = False
        except Exception:
            pass

        rid = ensure_request_id()
        set_request_id(rid)
        if user_id_getter is not None:
            try:
                uid = user_id_getter()
                set_user_id_hash(hash_user_id(uid, salt=client.config.user_id_salt))
            except Exception:
                pass

    @app.after_request
    def _pyloggle_after_request(response):
        # Successful request completion. Optionally emit a per-request timing event.
        if not bool(getattr(client.config, "emit_request_latency", False)):
            return response
        try:
            from flask import request, g

            if bool(getattr(g, "_pyloggle_had_exception", False)):
                return response

            self_route = getattr(request, "url_rule", None)
            route = getattr(self_route, "rule", None) or request.path
            method = request.method
            ua = request.headers.get("User-Agent", "")
            remote = request.remote_addr or ""
            rid = ensure_request_id()
            user_id_hash = get_user_id_hash() or ""

            start = float(getattr(g, "_pyloggle_request_start", 0.0) or 0.0)
            latency_ms = None
            if start > 0:
                latency_ms = str(int((time.perf_counter() - start) * 1000))

            status = int(getattr(response, "status_code", 200) or 200)
            level = "info"
            if status >= 500:
                level = "error"
            elif status >= 400:
                level = "warn"

            client.emit(
                {
                    "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                    "level": level,
                    "logger": "pyloggle.flask",
                    "message": "request",
                    "request_id": rid,
                    "http": {
                        "method": method,
                        "route": route,
                        "status": status,
                        "user_agent": ua,
                        "remote_addr": remote,
                    },
                    **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
                    "attrs": ({"latency_ms": latency_ms} if latency_ms is not None else {}),
                }
            )
        except Exception:
            return response
        return response

    @app.teardown_request
    def _pyloggle_teardown_request(exc: Optional[BaseException]):
        if exc is None:
            return
        try:
            from flask import request, g

            self_route = getattr(request, "url_rule", None)
            route = getattr(self_route, "rule", None) or request.path
            method = request.method
            ua = request.headers.get("User-Agent", "")
            remote = request.remote_addr or ""
            rid = ensure_request_id()
            user_id_hash = get_user_id_hash() or ""

            attrs = {}
            if bool(getattr(client.config, "emit_request_latency", False)):
                try:
                    start = float(getattr(g, "_pyloggle_request_start", 0.0) or 0.0)
                    if start > 0:
                        attrs["latency_ms"] = str(int((time.perf_counter() - start) * 1000))
                except Exception:
                    pass

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
                    "attrs": attrs,
                }
            )
        except Exception:
            return


