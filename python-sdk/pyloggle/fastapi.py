from __future__ import annotations

import time
from typing import Callable, Optional, Any

from .client import Client
from .context import ensure_request_id, set_request_id, set_user_id_hash, get_user_id_hash
from .stack import exception_to_payload
from .user import hash_user_id


class PyloggleFastAPIMiddleware:
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

        if capture_logs:
            try:
                from .logging_handler import install_pyloggle_logging

                install_pyloggle_logging(client)
            except Exception:
                pass

    async def __call__(self, scope, receive, send):
        if scope.get("type") != "http":
            return await self.app(scope, receive, send)

        # ensure request id for correlation
        request_id = ensure_request_id()
        set_request_id(request_id)

        # Best-effort: set user id hash early for this request if the app provides a getter.
        if self.user_id_getter is not None:
            try:
                uid = self.user_id_getter(scope)
                set_user_id_hash(hash_user_id(uid, salt=self.client.config.user_id_salt))
            except Exception:
                # never break the app due to identity extraction
                pass

        start = time.perf_counter()
        status_code = 500

        async def send_wrapper(message):
            nonlocal status_code
            if message.get("type") == "http.response.start":
                status_code = int(message.get("status", 500))
            await send(message)

        try:
            await self.app(scope, receive, send_wrapper)
        except Exception as exc:
            route = _route_path(scope)
            method = scope.get("method", "")
            headers = _headers_dict(scope)
            ua = headers.get("user-agent", "")
            remote = ""
            client_addr = scope.get("client")
            if client_addr and len(client_addr) >= 1:
                remote = str(client_addr[0])

            user_id_hash = get_user_id_hash() or ""

            attrs = {}
            if bool(getattr(self.client.config, "emit_request_latency", False)):
                attrs["latency_ms"] = str(int((time.perf_counter() - start) * 1000))

            self.client.emit(
                {
                    "ts": _iso_now(),
                    "level": "error",
                    "logger": "pyloggle.fastapi",
                    "message": str(exc),
                    "request_id": request_id,
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
            raise
        else:
            # Successful request completion. Optionally emit a per-request timing event.
            if not bool(getattr(self.client.config, "emit_request_latency", False)):
                return

            route = _route_path(scope)
            method = scope.get("method", "")
            headers = _headers_dict(scope)
            ua = headers.get("user-agent", "")
            remote = ""
            client_addr = scope.get("client")
            if client_addr and len(client_addr) >= 1:
                remote = str(client_addr[0])

            user_id_hash = get_user_id_hash() or ""
            latency_ms = str(int((time.perf_counter() - start) * 1000))

            level = "info"
            if int(status_code) >= 500:
                level = "error"
            elif int(status_code) >= 400:
                level = "warn"

            self.client.emit(
                {
                    "ts": _iso_now(),
                    "level": level,
                    "logger": "pyloggle.fastapi",
                    "message": "request",
                    "request_id": request_id,
                    "http": {
                        "method": method,
                        "route": route,
                        "status": int(status_code),
                        "user_agent": ua,
                        "remote_addr": remote,
                    },
                    **({"user": {"id_hash": user_id_hash}} if user_id_hash else {}),
                    "attrs": {"latency_ms": latency_ms},
                }
            )


def _route_path(scope) -> str:
    # Best-effort: Starlette stores route on scope["route"] with .path
    r = scope.get("route")
    p = getattr(r, "path", None)
    if p:
        return str(p)
    return scope.get("path", "")


def _iso_now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def _headers_dict(scope) -> dict[str, str]:
    # scope["headers"] is list[tuple[bytes, bytes]]
    out: dict[str, str] = {}
    for k, v in (scope.get("headers") or []):
        try:
            out[k.decode("latin1").lower()] = v.decode("latin1")
        except Exception:
            continue
    return out


