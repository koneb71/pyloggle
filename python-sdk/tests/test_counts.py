"""count_requests: every request counted once, none listed (see pyloggle/_counts.py)."""
import asyncio
import os

import pytest

from pyloggle import ClientConfig
from pyloggle._counts import RequestCounter, counts_url

T0 = 1_760_000_000 - (1_760_000_000 % 60)  # the start of a minute


def test_tallies_per_minute_and_endpoint():
    c = RequestCounter()
    c.add("get", "/orders", 200, 12.345, "u1", now=T0 + 1)
    c.add("GET", "/orders", 503, 30, "u2", now=T0 + 2)
    c.add("GET", "/orders", 200, None, "u1", now=T0 + 3)
    c.add("POST", "/orders", 201, 8, None, now=T0 + 4)
    c.add("GET", "/orders", 200, 5, "u3", now=T0 + 61)  # the next minute
    assert c.take(now=T0 + 59) == []  # the minute is still running
    lines = sorted(c.take(now=T0 + 60), key=lambda l: l["method"])
    assert lines == [
        {"minute": "2025-10-09T08:53:00Z", "method": "GET", "route": "/orders", "requests": 3, "errors": 1, "durations_ms": [12.35, 30.0], "users": ["u1", "u2"]},
        {"minute": "2025-10-09T08:53:00Z", "method": "POST", "route": "/orders", "requests": 1, "errors": 0, "durations_ms": [8.0]},
    ]
    assert c.take(now=T0 + 61, everything=True)[0]["requests"] == 1  # flush sends the running minute
    assert len(c) == 0


def test_a_full_tally_goes_at_once():
    c = RequestCounter(cap=3)
    for i in range(7):
        c.add("GET", "/", 200, 1, f"u{i}", now=T0)
    full = c.take(now=T0)
    assert [l["requests"] for l in full] == [3, 3]
    assert c.take(now=T0, everything=True)[0]["requests"] == 1


def test_open_endpoints_are_bounded():
    c = RequestCounter(max_open=2)
    for route in ("/a", "/b", "/c"):
        c.add("GET", route, 200, now=T0)
    assert len(c) == 2 and c.dropped == 1


def test_counts_url():
    assert counts_url("http://h:8080/v1/ingest") == "http://h:8080/v1/ingest/requests"
    assert counts_url("https://h/pyloggle/v1/ingest/?x=1") == "https://h/pyloggle/v1/ingest/requests?x=1"


def test_client_sends_tallies_with_its_app(make_client, fake_ingest):
    client = make_client(release="1.4.0")
    client.count_request("POST", "/orders", 201, 12.5, "u1")
    client.count_request("POST", "/orders", 500, 40, "u2")
    client.count_request("GET", "", 200, 3)  # matched no route: not counted
    assert client.flush(3.0)
    assert fake_ingest.events == []  # nothing listed
    (line,) = fake_ingest.counts
    assert line["service"] == "svc" and line["env"] == "test" and line["release"] == "1.4.0"
    assert (line["method"], line["route"], line["requests"], line["errors"]) == ("POST", "/orders", 2, 1)
    assert line["durations_ms"] == [12.5, 40.0] and line["users"] == ["u1", "u2"]
    sent = [r for r in fake_ingest.requests if r["path"].endswith("/requests")][0]
    assert sent["path"] == "/v1/ingest/requests" and sent["headers"]["x-api-key"] == "test-key"


def test_an_older_server_turns_counting_off(make_client, fake_ingest, monkeypatch):
    import pyloggle.client as pc

    warned = []
    monkeypatch.setattr(pc._log, "warning", lambda msg, *a: warned.append(msg % a))
    client = make_client()
    fake_ingest.statuses = [404]
    client.count_request("GET", "/", 200, 1)
    client.flush(3.0)
    assert not client._counting and any("does not take request counts" in w for w in warned)
    client.count_request("GET", "/", 200, 1)
    client.flush(3.0)
    assert len(fake_ingest.requests) == 1  # asked once, then never again


def test_counting_can_be_turned_off(make_client, fake_ingest):
    client = make_client(count_requests=False)
    client.count_request("GET", "/", 200, 1)
    client.flush(3.0)
    assert fake_ingest.requests == []


@pytest.mark.skipif(not hasattr(os, "fork"), reason="fork only")
def test_a_forked_child_starts_counting_afresh(make_client, fake_ingest):
    client = make_client()
    client.count_request("GET", "/parent", 200, 1)
    pid = os.fork()
    if pid == 0:  # child
        try:
            client.count_request("GET", "/child", 200, 1)
            ok = client.flush(5.0)
        finally:
            os._exit(0 if ok else 3)
    _, status = os.waitpid(pid, 0)
    assert os.WEXITSTATUS(status) == 0
    client.flush(3.0)
    routes = sorted(l["route"] for l in fake_ingest.counts)
    assert routes == ["/child", "/parent"]  # the child did not send the parent's tally again


# --- the integrations -------------------------------------------------------------------


class _Capture:
    def __init__(self, **cfg):
        self.config = ClientConfig(user_id_salt=None, **cfg)
        self.emitted = []
        self.counted = []

    def emit(self, ev):
        self.emitted.append(ev)

    def count_request(self, method, route, status, duration_ms=None, user_id_hash=None):
        self.counted.append((method, route, status))
        assert duration_ms is not None and duration_ms >= 0


def test_flask_counts_what_it_does_not_list():
    flask = pytest.importorskip("flask")
    from pyloggle.flask import init_app

    c = _Capture()
    app = flask.Flask(__name__)
    init_app(app, c, capture_logs=False)

    @app.route("/orders/<int:oid>", methods=["GET", "POST"])
    def order(oid):
        return {"ok": oid}

    @app.get("/fail")
    def fail():
        return "no", 503

    @app.get("/boom")
    def boom():
        raise ValueError("boom")

    with app.test_client() as tc:
        tc.get("/orders/1")
        tc.post("/orders/2", json={"a": 1})
        tc.options("/orders/1")  # ignored by default
        tc.get("/nowhere")  # matched no route
        tc.get("/fail")
        tc.get("/boom")  # the error event counts it
    assert c.counted == [("GET", "/orders/<int:oid>", 200), ("POST", "/orders/<int:oid>", 200), ("GET", "/fail", 503)]
    assert [e for e in c.emitted if e.get("message") == "request"] == []

    listed = _Capture(emit_request_latency=True)
    app2 = flask.Flask(__name__)
    init_app(app2, listed, capture_logs=False)
    app2.add_url_rule("/x", "x", lambda: "ok")
    with app2.test_client() as tc:
        tc.get("/x")
    assert listed.counted == [] and [e["message"] for e in listed.emitted] == ["request"]


def test_fastapi_counts_by_matched_route():
    pytest.importorskip("fastapi")
    from pyloggle.fastapi import PyloggleFastAPIMiddleware

    class _Route:
        path = "/orders/{oid}"

    async def app(scope, receive, send):
        if scope["path"].startswith("/orders/"):
            scope["route"] = _Route()
        await send({"type": "http.response.start", "status": 200 if "route" in scope else 404, "headers": []})
        await send({"type": "http.response.body", "body": b""})

    async def receive():
        return {"type": "http.request", "body": b"", "more_body": False}

    async def send(_m):
        return None

    c = _Capture()
    mw = PyloggleFastAPIMiddleware(app, c)  # type: ignore[arg-type]
    for method, path in (("GET", "/orders/7"), ("OPTIONS", "/orders/7"), ("GET", "/nowhere")):
        scope = {"type": "http", "method": method, "path": path, "query_string": b"", "headers": [], "client": ("127.0.0.1", 1), "server": ("x", 80)}
        asyncio.run(mw(scope, receive, send))
    assert c.counted == [("GET", "/orders/{oid}", 200)]


def test_django_counts_by_matched_route():
    pytest.importorskip("django")
    from tests import test_django_integration as tdj  # configures Django with its URLs

    import pyloggle.django as pdj

    c = _Capture()
    prev = pdj._client
    pdj.init(c)
    try:
        dj = tdj.DjangoClient(raise_request_exception=False)
        dj.get("/ok/")
        dj.get("/missing/")
        dj.options("/ok/")
        dj.get("/boom/3/")
    finally:
        pdj._client = prev
    assert c.counted == [("GET", "ok/", 200)]
