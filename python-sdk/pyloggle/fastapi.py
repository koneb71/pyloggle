from __future__ import annotations

import time
import uuid
from typing import Any, Callable, Optional

from . import _body
from . import scope as _scope
from ._request import begin_request, client_ip, end_request, headers_for_event
from .client import Client
from .context import get_user_id_hash, set_request_id, set_user_id_hash
from .otel import annotate_request_body
from .otel import current_ids as _otel_ids
from .otel import server_span as _otel_server_span
from .stack import captured_event_id, exception_to_payload, mark_captured
from .user import hash_user_id


def telemetry(**overrides: Any) -> dict:
    """The ``telemetry=`` dict for ``FastAPI(telemetry=...)`` (0.142+) when the SDK is also
    installed: FastAPI keeps its request and operation spans and its HTTP metrics, the SDK owns
    exceptions (``logs=False``, so a crash is one issue with locals and breadcrumbs, linked to
    the native trace), and exporters still come from the environment
    (``OTEL_EXPORTER_OTLP_ENDPOINT``, ``FASTAPI_OTEL_AUTO_CONFIGURE=true``).

        app = FastAPI(telemetry=pyloggle.fastapi.telemetry(exclude=lambda s: s["path"] == "/health"))
        app.add_middleware(PyloggleFastAPIMiddleware, client=client)
    """
    cfg: dict = {"logs": False}
    cfg.update(overrides)
    return cfg


class PyloggleFastAPIMiddleware:
    """Pure ASGI middleware (works with FastAPI, Starlette and any ASGI app): request scope,
    unhandled-exception capture (exactly once per request), optional request timing, and the
    asyncio exception hook for background tasks."""

    def __init__(
        self,
        app,
        client: Client,
        *,
        user_id_getter: Optional[Callable[[dict], Optional[Any]]] = None,
        capture_logs: bool = True,
    ):
        self.app = app
        self.client = client
        self.user_id_getter = user_id_getter
        self._asyncio_hooked = False

        if capture_logs:
            try:
                from .logging_handler import install_pyloggle_logging

                install_pyloggle_logging(client)
            except Exception:
                pass

    async def __call__(self, scope, receive, send):
        if scope.get("type") != "http":
            return await self.app(scope, receive, send)

        if not self._asyncio_hooked and bool(getattr(self.client.config, "install_excepthooks", True)):
            self._asyncio_hooked = True
            try:
                from . import hooks

                hooks.install_asyncio_hook(None, client=self.client)
            except Exception:
                pass

        headers = _headers_dict(scope)
        url = _pyloggle_get_full_url(scope)
        server_addr = _pyloggle_get_server_addr(scope)
        remote = ""
        client_addr = scope.get("client")
        if client_addr and len(client_addr) >= 1:
            remote = str(client_addr[0])
        http_base = {
            "method": scope.get("method", ""),
            "route": _route_path(scope),
            "url": url,
            "user_agent": headers.get("user-agent", ""),
            "remote_addr": remote,
            "server_addr": server_addr,
            "client_ip": client_ip(remote, headers, self.client.config),
        }
        qs = scope.get("query_string") or b""
        try:
            qs_str = qs if isinstance(qs, str) else bytes(qs).decode("latin1", errors="ignore")
        except Exception:
            qs_str = ""
        if qs_str:
            http_base["query"] = qs_str

        tap = _BodyTap(headers.get("content-type", ""), self.client.config) if _body.enabled(self.client.config) else None
        token, request_id = begin_request(
            config=self.client.config,
            headers=headers,
            http=http_base,
            read_body=tap.describe if tap is not None else None,
        )
        set_request_id(request_id)
        otel_tid, otel_sid = _otel_ids()
        if otel_tid:
            # FastAPI's native telemetry (and the contrib instrumentation) opened the request
            # span around the middleware stack: correlate with it, not with the upstream parent.
            sc = _scope.effective_scope()
            sc.trace_id, sc.span_id = otel_tid, otel_sid
        # Decided now, while the span is recording: it ends with the last body chunk, before
        # this call returns.
        otel_span = _otel_server_span()

        if self.user_id_getter is not None:
            try:
                uid = self.user_id_getter(scope)
                set_user_id_hash(hash_user_id(uid, salt=self.client.config.user_id_salt))
            except Exception:
                pass

        start = time.perf_counter()
        status_code: Optional[int] = None

        async def receive_to_app():
            msg = await receive()
            if tap is not None:
                tap.feed(msg)
            return msg

        body_on_span = False

        def annotate_span() -> None:
            nonlocal body_on_span
            if otel_span is None or tap is None or body_on_span:
                return
            body_on_span = True
            annotate_request_body(otel_span, self.client, *tap.describe())

        async def send_wrapper(message):
            nonlocal status_code
            if message.get("type") == "http.response.start":
                status_code = int(message.get("status", 500))
                annotate_span()  # the span ends with the response, so now
            await send(message)

        try:
            await self.app(scope, receive_to_app, send_wrapper)
        except Exception as exc:
            try:
                annotate_span()
                self._capture(exc, scope, headers, http_base, status_code, start, tap, request_id)
            except Exception:
                pass
            raise
        else:
            if not bool(getattr(self.client.config, "emit_request_latency", False)) or otel_span is not None:
                return  # the server span already yields the request line
            try:
                final_status = int(status_code if status_code is not None else 500)
                level = "info"
                if final_status >= 500:
                    level = "error"
                elif final_status >= 400:
                    level = "warn"
                duration_ms = round((time.perf_counter() - start) * 1000, 2)
                attrs: dict[str, Any] = {"latency_ms": str(int(duration_ms))}
                http = dict(http_base)
                http["route"] = _route_path(scope)  # the router fills scope["route"] after we started
                http["status"] = final_status
                if tap is not None:
                    body, truncated = tap.describe()
                    if body is not None:
                        http["body"] = body
                    if truncated:
                        attrs["request_body_truncated"] = True
                user_id_hash = get_user_id_hash() or ""
                self.client.emit(
                    {
                        "ts": _iso_now(),
                        "level": level,
                        "logger": "pyloggle.fastapi",
                        "message": "request",
                        "request_id": request_id,
                        "http": http,
                        **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
                        "duration_ms": duration_ms,
                        "attrs": attrs,
                    }
                )
            except Exception:
                pass
        finally:
            end_request(token)

    def _capture(self, exc, scope, headers, http_base, status_code, start, tap, request_id):
        if captured_event_id(exc):
            return
        if bool(getattr(self.client.config, "otel_owns_exceptions", False)):
            mark_captured(exc, "otel")  # the OTLP path reports it; log lines stay duplicates
            return
        attrs: dict[str, Any] = {}
        duration_ms = None
        if bool(getattr(self.client.config, "emit_request_latency", False)):
            duration_ms = round((time.perf_counter() - start) * 1000, 2)
            attrs["latency_ms"] = str(int(duration_ms))
        http = dict(http_base)
        http["route"] = _route_path(scope)
        http["status"] = int(status_code) if status_code is not None else 500
        hdrs = headers_for_event(headers, self.client.config)
        if hdrs:
            http["headers"] = hdrs
        if tap is not None:
            body, truncated = tap.describe()
            if body is not None:
                http["body"] = body
            if truncated:
                attrs["request_body_truncated"] = True
        user_id_hash = get_user_id_hash() or ""
        event_id = uuid.uuid4().hex
        ev = {
            "event_id": event_id,
            "ts": _iso_now(),
            "level": "error",
            "logger": "pyloggle.fastapi",
            "message": str(exc) or type(exc).__name__,
            "request_id": request_id,
            "exception": exception_to_payload(exc, config=self.client.config),
            "mechanism": {"type": "asgi", "handled": False},
            "http": http,
            **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
            **({"duration_ms": duration_ms} if duration_ms is not None else {}),
            "attrs": attrs,
        }
        mark_captured(exc, event_id)
        self.client.emit(ev)


# Alias for non-FastAPI ASGI apps.
PyloggleASGIMiddleware = PyloggleFastAPIMiddleware


def _pyloggle_get_full_url(scope) -> str:
    """
    Best-effort full URL for the current request, including query string.
    """
    try:
        scheme = str(scope.get("scheme") or "http")
    except Exception:
        scheme = "http"

    headers = _headers_dict(scope)
    host = headers.get("host", "")
    if not host:
        try:
            server = scope.get("server") or ()
            if server and len(server) >= 1:
                host = str(server[0])
                if len(server) >= 2 and server[1] is not None:
                    host = f"{host}:{int(server[1])}"
        except Exception:
            host = ""

    try:
        path = str(scope.get("root_path") or "") + str(scope.get("path") or "")
    except Exception:
        path = str(scope.get("path") or "")

    try:
        qs = scope.get("query_string") or b""
        if isinstance(qs, str):
            qs_str = qs
        else:
            qs_str = bytes(qs).decode("latin1", errors="ignore")
    except Exception:
        qs_str = ""

    if host:
        base = f"{scheme}://{host}{path}"
    else:
        base = path
    if qs_str:
        return f"{base}?{qs_str}"
    return base


def _pyloggle_get_server_addr(scope) -> str:
    """
    Best-effort server address (host[:port]) for the current ASGI request.

    Prefer scope["server"] (ASGI server endpoint) over Host header.
    """
    try:
        server = scope.get("server") or ()
        if not server or len(server) < 1:
            return ""
        host = server[0]
        port = server[1] if len(server) >= 2 else None
    except Exception:
        return ""

    try:
        host_s = str(host)
    except Exception:
        host_s = ""
    if not host_s:
        return ""

    def _wrap_ipv6(h: str) -> str:
        if ":" in h and not h.startswith("[") and not h.endswith("]"):
            return f"[{h}]"
        return h

    if port is None:
        return host_s
    try:
        port_i = int(port)
    except Exception:
        return host_s
    return f"{_wrap_ipv6(host_s)}:{port_i}"


class _BodyTap:
    """Keeps a copy of the request body as the app reads it, up to the read cap: the body a
    request line or an error records. Nothing is read that the app did not read itself."""

    def __init__(self, content_type: str, config: Any) -> None:
        self.content_type = content_type
        self.cap = _body.read_cap(config)
        self.buf = bytearray()
        self.size = 0
        self.done = False

    def feed(self, msg: Any) -> None:
        if not msg or msg.get("type") != "http.request":
            return
        chunk = msg.get("body") or b""
        if isinstance(chunk, (bytes, bytearray)) and chunk:
            self.size += len(chunk)
            room = self.cap - len(self.buf)
            if room > 0:
                self.buf.extend(chunk[:room])
        if not msg.get("more_body", False):
            self.done = True

    def describe(self) -> tuple:
        """(body, truncated) of what was read so far; never raises."""
        if not self.size:
            return None, False
        try:
            complete = self.done and self.size <= self.cap
            return _body.describe_body(bytes(self.buf), self.content_type, complete=complete, size=self.size if self.done else None)
        except Exception:
            return None, False


def _route_path(scope) -> str:
    # Best-effort: Starlette stores route on scope["route"] with .path
    r = scope.get("route")
    p = getattr(r, "path", None)
    if p:
        return str(p)
    return scope.get("path", "")


def _iso_now() -> str:
    return _scope.iso_now()


def _headers_dict(scope) -> dict[str, str]:
    # scope["headers"] is list[tuple[bytes, bytes]]
    out: dict[str, str] = {}
    for k, v in (scope.get("headers") or []):
        try:
            out[k.decode("latin1").lower()] = v.decode("latin1")
        except Exception:
            continue
    return out
