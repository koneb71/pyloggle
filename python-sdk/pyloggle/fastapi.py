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
    ):
        self.app = app
        self.client = client
        self.user_id_getter = user_id_getter

    async def __call__(self, scope, receive, send):
        if scope.get("type") != "http":
            return await self.app(scope, receive, send)

        rid = scope.get("headers", [])
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

        start = time.time()
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
            headers = dict((k.decode("latin1"), v.decode("latin1")) for k, v in (scope.get("headers") or []))
            ua = headers.get("user-agent", "")
            remote = ""
            client_addr = scope.get("client")
            if client_addr and len(client_addr) >= 1:
                remote = str(client_addr[0])

            user_id_hash = get_user_id_hash() or ""

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
                    "attrs": {
                        "latency_ms": str(int((time.time() - start) * 1000)),
                    },
                }
            )
            raise


def _route_path(scope) -> str:
    # Best-effort: Starlette stores route on scope["route"] with .path
    r = scope.get("route")
    p = getattr(r, "path", None)
    if p:
        return str(p)
    return scope.get("path", "")


def _iso_now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


