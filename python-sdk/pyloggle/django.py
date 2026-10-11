"""Django integration: settings-driven init, a request middleware (sync and async), unhandled
exception capture (exactly once per request), the authenticated user, and SQL breadcrumbs.

    # settings.py
    MIDDLEWARE = ["pyloggle.django.PyloggleMiddleware", *MIDDLEWARE]
    PYLOGGLE = {"service": "shop", "env": "prod", "emit_request_latency": True}

    # apps.py (or anywhere that runs once at startup)
    class ShopConfig(AppConfig):
        def ready(self):
            import pyloggle.django
            pyloggle.django.init()

The middleware initialises the client from ``settings.PYLOGGLE`` on its own when ``init()`` was
not called, so the one-line MIDDLEWARE change is enough for most apps.
"""
from __future__ import annotations

import sys
import time
import uuid
from contextlib import ExitStack
from typing import Any, Callable, Dict, Optional

from . import scope as _scope
from ._request import begin_request, client_ip, end_request, headers_for_event
from .client import Client
from .context import get_user_id_hash, set_request_id
from .otel import current_ids as _otel_ids
from .otel import server_span_active as _otel_server_span_active
from .stack import captured_event_id, exception_to_payload, mark_captured
from .user import hash_user_id

_client: Optional[Client] = None
_SIGNAL_UID = "pyloggle.django.got_request_exception"
_SQL_MESSAGE_LEN = 200


def init(client: Optional[Client] = None, *, force: bool = False, **overrides: Any) -> Client:
    """Create (once) the client from ``settings.PYLOGGLE`` merged with ``overrides`` and connect
    the exception signal. Pass ``client`` to reuse one built elsewhere."""
    global _client
    if _client is not None and not force and client is None:
        return _client
    if client is None:
        import pyloggle

        cfg: Dict[str, Any] = {}
        try:
            from django.conf import settings

            cfg.update(dict(getattr(settings, "PYLOGGLE", None) or {}))
        except Exception:
            pass
        cfg.update(overrides)
        client = pyloggle.init(**cfg)
    _client = client
    _connect_signal()
    return client


def get_client() -> Optional[Client]:
    if _client is not None:
        return _client
    import pyloggle

    return pyloggle.get_client()


def _connect_signal() -> None:
    try:
        from django.core.signals import got_request_exception

        got_request_exception.connect(_on_request_exception, dispatch_uid=_SIGNAL_UID, weak=False)
    except Exception:
        pass


def _on_request_exception(sender: Any = None, request: Any = None, **_extra: Any) -> None:
    """Django sends this for every unhandled exception before it logs it, so marking the
    exception captured here keeps the ``django.request`` log line from becoming a second issue."""
    exc = sys.exc_info()[1]
    try:
        request._pyloggle_had_exception = True  # the error event stands in for the request event
    except Exception:
        pass
    client = get_client()
    if client is None or exc is None or captured_event_id(exc):
        return
    if bool(getattr(client.config, "otel_owns_exceptions", False)):
        mark_captured(exc, "otel")
        return
    try:
        _capture(client, request, exc)
    except Exception:
        return


def _capture(client: Client, request: Any, exc: BaseException) -> None:
    cfg = client.config
    http = _request_http(client, request, status=500) if request is not None else {}
    attrs: Dict[str, Any] = {}
    ev: Dict[str, Any] = {
        "event_id": uuid.uuid4().hex,
        "ts": _scope.iso_now(),
        "level": "error",
        "logger": "pyloggle.django",
        "message": str(exc) or type(exc).__name__,
        "exception": exception_to_payload(exc, config=cfg),
        "mechanism": {"type": "django", "handled": False},
        "attrs": attrs,
    }
    if request is not None:
        rid = getattr(request, "_pyloggle_request_id", None)
        if rid:
            ev["request_id"] = rid
        start = float(getattr(request, "_pyloggle_start", 0.0) or 0.0)
        if start > 0:
            ms = _elapsed_ms(start)
            ev["duration_ms"] = ms
            attrs["latency_ms"] = str(int(ms))
        body, truncated = _request_body(client, request)
        if body is not None:
            http["body"] = body
        if truncated:
            attrs["request_body_truncated"] = True
    if http:
        ev["http"] = http
    user_id_hash = get_user_id_hash() or ""
    if user_id_hash:
        ev["user"] = {"id_hash": user_id_hash}
    mark_captured(exc, ev["event_id"])
    client.emit(ev)


class PyloggleMiddleware:
    """Request scope (request id, trace ids, route, user), the optional ``request`` timing
    event and SQL breadcrumbs. Works in WSGI and ASGI stacks. Put it first in MIDDLEWARE so
    exceptions raised by other middleware are captured too."""

    sync_capable = True
    async_capable = True

    def __init__(self, get_response: Callable[..., Any]):
        self.get_response = get_response
        if get_client() is None:
            init()
        self._async = _is_coroutine_function(get_response)
        if self._async:
            _mark_coroutine_function(self)

    # --- entry points --------------------------------------------------------------------

    def __call__(self, request: Any) -> Any:
        if self._async:
            return self.__acall__(request)
        client = get_client()
        if client is None:
            return self.get_response(request)
        token = self._begin(client, request)
        try:
            with self._sql_breadcrumbs(client):
                response = self.get_response(request)
            self._finish(client, request, response)
            return response
        finally:
            end_request(token)

    async def __acall__(self, request: Any) -> Any:
        client = get_client()
        if client is None:
            return await self.get_response(request)
        token = self._begin(client, request)
        try:
            with self._sql_breadcrumbs(client):
                response = await self.get_response(request)
            self._finish(client, request, response)
            return response
        finally:
            end_request(token)

    def process_view(self, request: Any, view_func: Any, view_args: Any, view_kwargs: Any) -> None:
        """Runs once the URL is resolved: the route template and the authenticated user."""
        client = get_client()
        if client is None:
            return None
        sc = _scope.current_scope()
        if sc is None:
            return None
        route = _route(request)
        if route:
            sc.transaction = route
            if sc.http is not None:
                sc.http["route"] = route
        self._set_user(client, request, sc)
        return None

    # --- pieces ----------------------------------------------------------------------------

    def _begin(self, client: Client, request: Any) -> Any:
        headers = _headers(request)
        http = _request_http(client, request)
        http.pop("headers", None)  # headers are attached on error events only
        token, rid = begin_request(config=client.config, headers=headers, http=http)
        set_request_id(rid)
        otel_tid, otel_sid = _otel_ids()
        if otel_tid:
            sc = _scope.effective_scope()
            sc.trace_id, sc.span_id = otel_tid, otel_sid
        try:
            request._pyloggle_request_id = rid
            request._pyloggle_start = time.perf_counter()
            request._pyloggle_otel_server = _otel_server_span_active()
        except Exception:
            pass
        return token

    def _finish(self, client: Client, request: Any, response: Any) -> None:
        if not bool(getattr(client.config, "emit_request_latency", False)):
            return
        if bool(getattr(request, "_pyloggle_had_exception", False)) or bool(getattr(request, "_pyloggle_otel_server", False)):
            return  # the error event, or the server span, already covers the request
        try:
            start = float(getattr(request, "_pyloggle_start", 0.0) or 0.0)
            ms = _elapsed_ms(start) if start > 0 else None
            status = int(getattr(response, "status_code", 200) or 200)
            level = "info"
            if status >= 500:
                level = "error"
            elif status >= 400:
                level = "warn"
            http = _request_http(client, request, status=status)
            http.pop("headers", None)
            body, truncated = _request_body(client, request)
            if body is not None:
                http["body"] = body
            attrs: Dict[str, Any] = {}
            if truncated:
                attrs["request_body_truncated"] = True
            ev: Dict[str, Any] = {
                "ts": _scope.iso_now(),
                "level": level,
                "logger": "pyloggle.django",
                "message": "request",
                "request_id": getattr(request, "_pyloggle_request_id", None) or "",
                "http": http,
                "attrs": attrs,
            }
            if ms is not None:
                ev["duration_ms"] = ms
                attrs["latency_ms"] = str(int(ms))
            user_id_hash = get_user_id_hash() or ""
            if user_id_hash:
                ev["user"] = {"id_hash": user_id_hash}
            client.emit(ev)
        except Exception:
            return

    def _set_user(self, client: Client, request: Any, sc: _scope.Scope) -> None:
        try:
            user = getattr(request, "user", None)
            if user is None or not bool(getattr(user, "is_authenticated", False)):
                return
            pk = getattr(user, "pk", None)
            if pk is None:
                return
            info: Dict[str, Any] = {"id_hash": hash_user_id(pk, salt=client.config.user_id_salt), "id": pk}
            email = getattr(user, "email", None)
            if email:
                info["email"] = email
            get_username = getattr(user, "get_username", None)
            username = get_username() if callable(get_username) else getattr(user, "username", None)
            if username:
                info["username"] = username
            sc.user = info
        except Exception:
            return

    def _sql_breadcrumbs(self, client: Client) -> ExitStack:
        """Wrap every configured database connection for the request (the wrapper object is
        cheap: Django only connects on the first query)."""
        stack = ExitStack()
        if not bool(getattr(client.config, "db_breadcrumbs", True)):
            return stack
        try:
            from django.db import connections

            for conn in connections.all():
                stack.enter_context(conn.execute_wrapper(_sql_wrapper))
        except Exception:
            pass
        return stack


def _sql_wrapper(execute: Callable[..., Any], sql: Any, params: Any, many: bool, context: Dict[str, Any]) -> Any:
    """execute_wrapper hook: one breadcrumb per statement, the SQL truncated, never the params."""
    start = time.perf_counter()
    try:
        result = execute(sql, params, many, context)
    except Exception as exc:
        _db_crumb(sql, context, start, level="error", error=exc)
        raise
    _db_crumb(sql, context, start)
    return result


def _db_crumb(sql: Any, context: Dict[str, Any], start: float, *, level: str = "info", error: Optional[BaseException] = None) -> None:
    try:
        text = str(sql).strip()
        if len(text) > _SQL_MESSAGE_LEN:
            text = text[:_SQL_MESSAGE_LEN] + "…"
        data: Dict[str, Any] = {"duration_ms": round((time.perf_counter() - start) * 1000, 2)}
        conn = context.get("connection") if isinstance(context, dict) else None
        alias = getattr(conn, "alias", None)
        if alias:
            data["db"] = str(alias)
        if error is not None:
            data["error"] = f"{type(error).__name__}: {str(error)[:200]}"
        _scope.add_breadcrumb(text, category="db.query", type="query", level=level, data=data)
    except Exception:
        return


# --- request helpers -------------------------------------------------------------------------


def _elapsed_ms(start: float) -> float:
    """Milliseconds since start with two decimals: a 0.4 ms request is still a request."""
    return round((time.perf_counter() - start) * 1000, 2)


def _headers(request: Any) -> Dict[str, str]:
    try:
        return {str(k).lower(): str(v) for k, v in request.headers.items()}
    except Exception:
        meta = getattr(request, "META", None) or {}
        out: Dict[str, str] = {}
        for k, v in meta.items():
            if isinstance(k, str) and k.startswith("HTTP_"):
                out[k[5:].replace("_", "-").lower()] = str(v)
        return out


def _route(request: Any) -> str:
    match = getattr(request, "resolver_match", None)
    route = getattr(match, "route", None) if match is not None else None
    if route:
        return str(route)
    return ""


def _full_url(request: Any) -> str:
    try:
        return str(request.build_absolute_uri())
    except Exception:
        try:
            return str(request.get_full_path())
        except Exception:
            return ""


def _server_addr(request: Any) -> str:
    meta = getattr(request, "META", None) or {}
    name = str(meta.get("SERVER_NAME") or "")
    port = meta.get("SERVER_PORT")
    if not name:
        return ""
    if port in (None, ""):
        return name
    if ":" in name and not name.startswith("["):
        name = f"[{name}]"
    return f"{name}:{port}"


def _request_http(client: Client, request: Any, *, status: Optional[int] = None) -> Dict[str, Any]:
    headers = _headers(request)
    meta = getattr(request, "META", None) or {}
    remote = str(meta.get("REMOTE_ADDR") or "")
    http: Dict[str, Any] = {
        "method": str(getattr(request, "method", "") or ""),
        "route": _route(request) or str(getattr(request, "path", "") or ""),
        "url": _full_url(request),
        "user_agent": headers.get("user-agent", ""),
        "remote_addr": remote,
        "server_addr": _server_addr(request),
        "client_ip": client_ip(remote, headers, client.config),
    }
    if status is not None:
        http["status"] = int(status)
    qs = str(meta.get("QUERY_STRING") or "")
    if qs:
        http["query"] = qs
    hdrs = headers_for_event(headers, client.config)
    if hdrs:
        http["headers"] = hdrs
    return http


def _request_body(client: Client, request: Any) -> tuple:
    """(body, truncated) when body capture is enabled; a stream Django already consumed
    (form parsing) yields nothing rather than an error."""
    if not bool(getattr(client.config, "emit_request_body", False)):
        return None, False
    try:
        max_bytes = int(getattr(client.config, "max_request_body_bytes", 4096) or 4096)
    except Exception:
        max_bytes = 4096
    if max_bytes <= 0:
        return None, False
    try:
        raw = bytes(request.body or b"")
    except Exception:
        return None, False
    if not raw:
        return None, False
    truncated = len(raw) > max_bytes
    if truncated:
        raw = raw[:max_bytes]
    if not truncated:
        ct = str(getattr(request, "content_type", "") or "").lower()
        if "json" in ct or raw.lstrip().startswith((b"{", b"[")):
            try:
                import json

                return json.loads(raw.decode("utf-8")), False
            except Exception:
                pass
    return raw.decode("utf-8", errors="replace"), truncated


def _is_coroutine_function(fn: Any) -> bool:
    try:
        from asgiref.sync import iscoroutinefunction

        return bool(iscoroutinefunction(fn))
    except Exception:
        import asyncio

        return asyncio.iscoroutinefunction(fn)


def _mark_coroutine_function(obj: Any) -> None:
    try:
        from asgiref.sync import markcoroutinefunction

        markcoroutinefunction(obj)
    except Exception:
        try:
            import asyncio

            obj._is_coroutine = asyncio.coroutines._is_coroutine  # type: ignore[attr-defined]
        except Exception:
            pass
