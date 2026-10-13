"""ignore_requests: requests that never get a request line (preflight and HEAD by default)."""
import asyncio

import pytest

from pyloggle import ClientConfig
from pyloggle._request import ignored_request


class _Capture:
    def __init__(self, **cfg):
        self.config = ClientConfig(user_id_salt=None, emit_request_latency=True, **cfg)
        self.emitted = []

    def emit(self, ev):
        self.emitted.append(ev)

    def lines(self):
        return [(e["http"]["method"], e["http"]["route"]) for e in self.emitted if e.get("message") == "request"]


def test_rules():
    cfg = ClientConfig()
    assert ignored_request(cfg, "OPTIONS", "/orders")
    assert ignored_request(cfg, "head", "/")
    assert not ignored_request(cfg, "GET", "/orders")
    assert not ignored_request(cfg, "OPTIONS", "/orders", status=503)  # failures are always sent

    cfg = ClientConfig(ignore_requests=["/health", "GET /static/*", "* /orders/<int:id>"])
    assert ignored_request(cfg, "POST", "/health")
    assert ignored_request(cfg, "GET", "/static/app.css")
    assert not ignored_request(cfg, "POST", "/static/upload")
    assert ignored_request(cfg, "PUT", "/orders/81723", route="/orders/<int:id>")  # by route
    assert not ignored_request(cfg, "OPTIONS", "/orders")  # the defaults were replaced
    assert not ignored_request(ClientConfig(ignore_requests=()), "OPTIONS", "/")


def test_flask_skips_preflight_and_head():
    flask = pytest.importorskip("flask")
    from pyloggle.flask import init_app

    c = _Capture(ignore_requests=("OPTIONS *", "HEAD *", "/health"))
    app = flask.Flask(__name__)
    init_app(app, c, capture_logs=False)

    @app.route("/orders", methods=["GET", "POST"])
    def orders():
        return {"ok": True}

    @app.get("/health")
    def health():
        return "ok"

    with app.test_client() as tc:
        tc.options("/orders")
        tc.head("/orders")
        tc.get("/health")
        tc.post("/orders", json={"a": 1})
    assert c.lines() == [("POST", "/orders")]


def test_asgi_skips_preflight():
    pytest.importorskip("fastapi")
    from pyloggle.fastapi import PyloggleFastAPIMiddleware

    async def app(scope, receive, send):
        await send({"type": "http.response.start", "status": 200, "headers": []})
        await send({"type": "http.response.body", "body": b""})

    async def receive():
        return {"type": "http.request", "body": b"", "more_body": False}

    async def send(_m):
        return None

    c = _Capture()
    mw = PyloggleFastAPIMiddleware(app, c)  # type: ignore[arg-type]
    for method in ("OPTIONS", "GET"):
        scope = {"type": "http", "method": method, "path": "/orders", "query_string": b"", "headers": [], "client": ("127.0.0.1", 1), "server": ("x", 80)}
        asyncio.run(mw(scope, receive, send))
    assert c.lines() == [("GET", "/orders")]


def test_django_skips_preflight():
    pytest.importorskip("django")
    from tests import test_django_integration as tdj  # configures Django with its URLs

    import pyloggle.django as pdj

    c = _Capture()
    prev = pdj._client
    pdj.init(c)
    try:
        dj = tdj.DjangoClient()
        dj.options("/ok/")
        dj.get("/ok/")
    finally:
        pdj._client = prev
    assert [m for m, _ in c.lines()] == ["GET"]
