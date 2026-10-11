import asyncio

import pytest

fastapi = pytest.importorskip("fastapi")
from fastapi.testclient import TestClient  # noqa: E402

from pyloggle import scope  # noqa: E402
from pyloggle.fastapi import PyloggleFastAPIMiddleware  # noqa: E402


class _Capture:
    def __init__(self, **cfg):
        from pyloggle import ClientConfig

        self.config = ClientConfig(**cfg)
        self.emitted = []

    def emit(self, ev):
        self.emitted.append(ev)


def _app(client):
    app = fastapi.FastAPI()
    app.add_middleware(PyloggleFastAPIMiddleware, client=client, capture_logs=False)

    @app.get("/ok")
    def ok():
        return {"ok": True}

    @app.post("/boom/{item}")
    def boom(item: str):
        raise RuntimeError(f"boom {item}")

    @app.get("/peek")
    def peek():
        sc = scope.effective_scope()
        return {"rid": sc.request_id, "trace": sc.trace_id, "route": sc.http["route"]}

    return app


def test_one_event_with_route_template_headers_and_status():
    c = _Capture(user_id_salt=None)
    app = _app(c)
    with TestClient(app, raise_server_exceptions=False) as tc:
        r = tc.post("/boom/42?q=1", headers={"X-Request-ID": "rid-9", "Authorization": "Bearer x", "User-Agent": "ua"}, json={"k": "v"})
    assert r.status_code == 500
    assert len(c.emitted) == 1
    ev = c.emitted[0]
    assert ev["request_id"] == "rid-9"
    assert ev["http"]["route"] == "/boom/{item}" and ev["http"]["method"] == "POST" and ev["http"]["status"] == 500
    assert ev["http"]["query"] == "q=1" and ev["http"]["headers"]["authorization"] == "[REDACTED]"
    assert ev["mechanism"] == {"type": "asgi", "handled": False}
    assert ev["exception"]["message"] == "boom 42"
    assert scope.current_scope() is None


def test_scope_per_request():
    c = _Capture(user_id_salt=None)
    app = _app(c)
    with TestClient(app) as tc:
        a = tc.get("/peek", headers={"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}).json()
        b = tc.get("/peek").json()
    assert a["trace"] == "4bf92f3577b34da6a3ce929d0e0e4736" and b["trace"] is None
    assert a["rid"] != b["rid"] and a["route"] == "/peek"


def test_cancelled_requests_are_not_reported():
    c = _Capture(user_id_salt=None)

    async def app(scope_, receive, send):
        raise asyncio.CancelledError()

    mw = PyloggleFastAPIMiddleware(app, c, capture_logs=False)
    scope_ = {"type": "http", "method": "GET", "path": "/x", "headers": [], "client": ("127.0.0.1", 1)}

    async def receive():
        return {"type": "http.request"}

    async def send(m):
        pass

    with pytest.raises(asyncio.CancelledError):
        asyncio.run(mw(scope_, receive, send))
    assert c.emitted == []
    assert scope.current_scope() is None
