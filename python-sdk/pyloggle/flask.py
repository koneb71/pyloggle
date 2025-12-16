from __future__ import annotations

import json
import time
from typing import Optional, Callable, Any

from .client import Client
from .context import ensure_request_id, set_request_id, set_user_id_hash, get_user_id_hash
from .stack import exception_to_payload
from .user import hash_user_id


def _pyloggle_get_full_url(request) -> str:
    """
    Best-effort full URL for the current request, including query string.
    """
    try:
        url = getattr(request, "url", "") or ""
        return str(url)
    except Exception:
        return ""


def _pyloggle_get_server_addr(request) -> str:
    """
    Best-effort server address (host[:port]) for the current Flask/Werkzeug request.

    Prefer WSGI environ server fields; fall back to request.host.
    """
    try:
        env = getattr(request, "environ", None) or {}
    except Exception:
        env = {}

    name = ""
    port = None
    try:
        name = str(env.get("SERVER_NAME") or env.get("SERVER_ADDR") or "")
    except Exception:
        name = ""
    try:
        port = env.get("SERVER_PORT")
    except Exception:
        port = None

    def _wrap_ipv6(h: str) -> str:
        if ":" in h and not h.startswith("[") and not h.endswith("]"):
            return f"[{h}]"
        return h

    if name:
        if port is None:
            return name
        try:
            port_i = int(port)
        except Exception:
            return name
        return f"{_wrap_ipv6(name)}:{port_i}"

    # Fallback: Werkzeug's host string (may come from Host header).
    try:
        return str(getattr(request, "host", "") or "")
    except Exception:
        return ""


def _pyloggle_capture_request_body(client: Client, request) -> tuple[object | None, bool]:
    """
    Capture request body/payload in a size-limited way. Returns (body, truncated).

    - Only captures when client.config.emit_request_body is truthy.
    - Enforces client.config.max_request_body_bytes.
    - Prefers JSON decoding when possible and when not truncated.
    """
    if not bool(getattr(client.config, "emit_request_body", False)):
        return None, False

    try:
        max_bytes = int(getattr(client.config, "max_request_body_bytes", 4096) or 4096)
    except Exception:
        max_bytes = 4096
    if max_bytes <= 0:
        return None, False

    try:
        raw: bytes = request.get_data(cache=True, as_text=False) or b""
    except Exception:
        return None, False
    if not raw:
        return None, False

    truncated = len(raw) > max_bytes
    if truncated:
        raw = raw[:max_bytes]

    # Only attempt JSON decode when we have the complete payload (not truncated).
    if not truncated:
        try:
            ct = (getattr(request, "mimetype", "") or "").lower()
        except Exception:
            ct = ""
        looks_json = "json" in ct
        if not looks_json:
            try:
                looks_json = raw.lstrip().startswith((b"{", b"["))
            except Exception:
                looks_json = False

        if looks_json:
            try:
                return json.loads(raw.decode("utf-8")), False
            except Exception:
                pass

    # Fallback: best-effort UTF-8 text.
    try:
        return raw.decode("utf-8", errors="replace"), truncated
    except Exception:
        return None, truncated


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
            server_addr = _pyloggle_get_server_addr(request)
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

            url = _pyloggle_get_full_url(request)
            body, body_truncated = _pyloggle_capture_request_body(client, request)
            attrs: dict[str, Any] = ({"latency_ms": latency_ms} if latency_ms is not None else {})
            if body_truncated:
                attrs["request_body_truncated"] = True

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
                        "url": url,
                        "user_agent": ua,
                        "remote_addr": remote,
                        "server_addr": server_addr,
                        **({"body": body} if body is not None else {}),
                    },
                    **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
                    "attrs": attrs,
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
            server_addr = _pyloggle_get_server_addr(request)
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

            url = _pyloggle_get_full_url(request)
            body, body_truncated = _pyloggle_capture_request_body(client, request)
            if body_truncated:
                attrs["request_body_truncated"] = True

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
                        "url": url,
                        "user_agent": ua,
                        "remote_addr": remote,
                        "server_addr": server_addr,
                        **({"body": body} if body is not None else {}),
                    },
                    **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
                    "attrs": attrs,
                }
            )
        except Exception:
            return


