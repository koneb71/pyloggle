from __future__ import annotations

import json
import time
import uuid
from typing import Any, Callable, Optional

from . import scope as _scope
from ._request import begin_request, client_ip, end_request, headers_for_event
from .client import Client
from .context import ensure_request_id, get_user_id_hash, set_request_id, set_user_id_hash
from .stack import captured_event_id, exception_to_payload, mark_captured
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

    try:
        return raw.decode("utf-8", errors="replace"), truncated
    except Exception:
        return None, truncated


def _iso_now() -> str:
    return _scope.iso_now()


def _request_http(client: Client, request, *, status: Optional[int] = None) -> dict:
    """The http block for an event, from the Flask request."""
    try:
        headers = {str(k): str(v) for k, v in request.headers.items()}
    except Exception:
        headers = {}
    route_rule = getattr(request, "url_rule", None)
    route = getattr(route_rule, "rule", None) or request.path
    remote = request.remote_addr or ""
    http: dict = {
        "method": request.method,
        "route": route,
        "url": _pyloggle_get_full_url(request),
        "user_agent": headers.get("User-Agent", "") or request.headers.get("User-Agent", ""),
        "remote_addr": remote,
        "server_addr": _pyloggle_get_server_addr(request),
        "client_ip": client_ip(remote, {k.lower(): v for k, v in headers.items()}, client.config),
    }
    if status is not None:
        http["status"] = int(status)
    try:
        qs = request.query_string.decode("latin1") if isinstance(request.query_string, bytes) else str(request.query_string or "")
    except Exception:
        qs = ""
    if qs:
        http["query"] = qs
    hdrs = headers_for_event({k.lower(): v for k, v in headers.items()}, client.config)
    if hdrs:
        http["headers"] = hdrs
    return http


def init_app(
    app,
    client: Client,
    *,
    user_id_getter: Optional[Callable[[], Optional[Any]]] = None,
    capture_logs: bool = True,
) -> None:
    """Instrument a Flask app: request scope (request id, trace id, user, http context,
    breadcrumbs), unhandled-exception capture (exactly once per request) and optional
    per-request timing events."""
    if capture_logs:
        try:
            from .logging_handler import install_pyloggle_logging

            install_pyloggle_logging(client)
        except Exception:
            pass

    try:
        from flask import got_request_exception  # type: ignore

        def _pyloggle_capture_exception(_sender, exception, **_extra):
            try:
                from flask import g, request

                g._pyloggle_had_exception = True
                if exception is None or captured_event_id(exception):
                    return
                attrs: dict = {}
                duration_ms = None
                if bool(getattr(client.config, "emit_request_latency", False)):
                    start = float(getattr(g, "_pyloggle_request_start", 0.0) or 0.0)
                    if start > 0:
                        duration_ms = round((time.perf_counter() - start) * 1000, 2)
                        attrs["latency_ms"] = str(int(duration_ms))
                http = _request_http(client, request, status=500)
                body, body_truncated = _pyloggle_capture_request_body(client, request)
                if body is not None:
                    http["body"] = body
                if body_truncated:
                    attrs["request_body_truncated"] = True
                user_id_hash = get_user_id_hash() or ""
                event_id = uuid.uuid4().hex
                ev = {
                    "event_id": event_id,
                    "ts": _iso_now(),
                    "level": "error",
                    "logger": "pyloggle.flask",
                    "message": str(exception) or type(exception).__name__,
                    "request_id": ensure_request_id(),
                    "exception": exception_to_payload(exception, config=client.config),
                    "mechanism": {"type": "flask", "handled": False},
                    "http": http,
                    **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
                    **({"duration_ms": duration_ms} if duration_ms is not None else {}),
                    "attrs": attrs,
                }
                mark_captured(exception, event_id)
                client.emit(ev)
            except Exception:
                return

        # blinker keeps receivers weakly by default; the closure must outlive init_app().
        got_request_exception.connect(_pyloggle_capture_exception, app, weak=False)
    except Exception:
        pass

    @app.before_request
    def _pyloggle_before_request():
        from flask import g, request

        try:
            headers = {str(k).lower(): str(v) for k, v in request.headers.items()}
        except Exception:
            headers = {}
        try:
            http = _request_http(client, request)
            http.pop("headers", None)  # headers are attached on error events only
        except Exception:
            http = {}
        token, rid = begin_request(config=client.config, headers=headers, http=http)
        try:
            g._pyloggle_scope_token = token
            g._pyloggle_request_start = time.perf_counter()
            g._pyloggle_had_exception = False
        except Exception:
            pass
        set_request_id(rid)
        if user_id_getter is not None:
            try:
                uid = user_id_getter()
                set_user_id_hash(hash_user_id(uid, salt=client.config.user_id_salt))
            except Exception:
                pass

    @app.after_request
    def _pyloggle_after_request(response):
        if not bool(getattr(client.config, "emit_request_latency", False)):
            return response
        try:
            from flask import g, request

            if bool(getattr(g, "_pyloggle_had_exception", False)):
                return response
            start = float(getattr(g, "_pyloggle_request_start", 0.0) or 0.0)
            duration_ms = round((time.perf_counter() - start) * 1000, 2) if start > 0 else None
            latency_ms = str(int(duration_ms)) if duration_ms is not None else None
            status = int(getattr(response, "status_code", 200) or 200)
            level = "info"
            if status >= 500:
                level = "error"
            elif status >= 400:
                level = "warn"
            http = _request_http(client, request, status=status)
            http.pop("headers", None)
            body, body_truncated = _pyloggle_capture_request_body(client, request)
            if body is not None:
                http["body"] = body
            attrs: dict = {"latency_ms": latency_ms} if latency_ms is not None else {}
            if body_truncated:
                attrs["request_body_truncated"] = True
            user_id_hash = get_user_id_hash() or ""
            client.emit(
                {
                    "ts": _iso_now(),
                    "level": level,
                    "logger": "pyloggle.flask",
                    "message": "request",
                    "request_id": ensure_request_id(),
                    "http": http,
                    **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
                    **({"duration_ms": duration_ms} if duration_ms is not None else {}),
                    "attrs": attrs,
                }
            )
        except Exception:
            return response
        return response

    @app.teardown_request
    def _pyloggle_teardown_request(exc: Optional[BaseException]):
        try:
            from flask import g

            token = getattr(g, "_pyloggle_scope_token", None)
            g._pyloggle_scope_token = None
        except Exception:
            token = None
        end_request(token)
