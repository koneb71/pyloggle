from __future__ import annotations

import json
import time
import uuid
from typing import Any, Callable, Optional

from . import scope as _scope
from ._request import begin_request, client_ip, end_request, headers_for_event
from .client import Client
from .context import get_user_id_hash, set_request_id, set_user_id_hash
from .stack import captured_event_id, exception_to_payload, mark_captured
from .user import hash_user_id


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

        token, request_id = begin_request(config=self.client.config, headers=headers, http=http_base)
        set_request_id(request_id)

        if self.user_id_getter is not None:
            try:
                uid = self.user_id_getter(scope)
                set_user_id_hash(hash_user_id(uid, salt=self.client.config.user_id_salt))
            except Exception:
                pass

        start = time.perf_counter()
        status_code: Optional[int] = None

        body_buf: bytearray | None = None
        body_truncated = False
        max_body_bytes = 0
        if bool(getattr(self.client.config, "emit_request_body", False)):
            try:
                max_body_bytes = int(getattr(self.client.config, "max_request_body_bytes", 4096) or 4096)
            except Exception:
                max_body_bytes = 4096
            if max_body_bytes > 0:
                body_buf = bytearray()

                async def receive_wrapper():
                    nonlocal body_truncated
                    msg = await receive()
                    if msg and msg.get("type") == "http.request" and body_buf is not None:
                        b = msg.get("body") or b""
                        if isinstance(b, bytearray):
                            b = bytes(b)
                        if isinstance(b, (bytes,)) and b:
                            remaining = max(0, max_body_bytes - len(body_buf))
                            if remaining > 0:
                                body_buf.extend(b[:remaining])
                            if len(b) > remaining:
                                body_truncated = True
                    return msg

                receive_to_app = receive_wrapper
            else:
                receive_to_app = receive
        else:
            receive_to_app = receive

        async def send_wrapper(message):
            nonlocal status_code
            if message.get("type") == "http.response.start":
                status_code = int(message.get("status", 500))
            await send(message)

        try:
            await self.app(scope, receive_to_app, send_wrapper)
        except Exception as exc:
            try:
                self._capture(exc, scope, headers, http_base, status_code, start, body_buf, body_truncated, request_id)
            except Exception:
                pass
            raise
        else:
            if not bool(getattr(self.client.config, "emit_request_latency", False)):
                return
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
                if body_buf is not None and len(body_buf) > 0:
                    body_obj, body_truncated2 = _pyloggle_decode_request_body(scope, bytes(body_buf), body_truncated)
                    if body_truncated2:
                        body_truncated = True
                    if body_obj is not None:
                        http["body"] = body_obj
                if body_truncated:
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

    def _capture(self, exc, scope, headers, http_base, status_code, start, body_buf, body_truncated, request_id):
        if captured_event_id(exc):
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
        if body_buf is not None and len(body_buf) > 0:
            body_obj, body_truncated2 = _pyloggle_decode_request_body(scope, bytes(body_buf), body_truncated)
            if body_truncated2:
                body_truncated = True
            if body_obj is not None:
                http["body"] = body_obj
        if body_truncated:
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


def _pyloggle_decode_request_body(scope, raw: bytes, truncated: bool) -> tuple[object | None, bool]:
    """
    Decode a request body captured from ASGI receive() buffers.

    - Prefers JSON decoding when possible and when not truncated.
    - Falls back to best-effort UTF-8 text.
    """
    if not raw:
        return None, False

    looks_json = False
    if not truncated:
        try:
            headers = _headers_dict(scope)
            ct = (headers.get("content-type", "") or "").lower()
        except Exception:
            ct = ""
        if "json" in ct:
            looks_json = True
        else:
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
