import asyncio
import contextvars
import threading

import pytest

import pyloggle
from pyloggle import scope
from pyloggle.context import ensure_request_id, get_request_id, parse_traceparent


def test_global_vs_request_scope_and_push_scope():
    pyloggle.set_tag("team", "core")
    pyloggle.add_breadcrumb("boot", category="app")
    token = scope.push_scope()
    try:
        assert scope.effective_scope().tags == {"team": "core"}  # inherited
        pyloggle.set_tag("req", "1")
        assert scope.global_scope().tags == {"team": "core"}  # not leaked
    finally:
        scope.reset_scope(token)
    assert "req" not in scope.effective_scope().tags
    with pyloggle.push_scope():
        pyloggle.set_user({"id": "42", "email": "a@b.c"})
        assert scope.effective_scope().user["id_hash"]
    assert scope.global_scope().user is None


def test_threads_do_not_see_request_scope_but_do_see_global():
    pyloggle.set_tag("g", "1")
    token = scope.push_scope()
    pyloggle.set_tag("r", "1")
    seen = {}

    def worker():
        seen["tags"] = dict(scope.effective_scope().tags)

    t = threading.Thread(target=worker)
    t.start()
    t.join()
    scope.reset_scope(token)
    assert seen["tags"] == {"g": "1"}


def test_asyncio_tasks_isolate_scopes():
    async def task(n):
        token = scope.push_scope()
        ensure_request_id()
        await asyncio.sleep(0.01)
        rid = get_request_id()
        scope.reset_scope(token)
        return rid

    async def main():
        return await asyncio.gather(task(1), task(2))

    a, b = asyncio.run(main())
    assert a and b and a != b


def test_traceparent():
    assert parse_traceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01") == ("4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7")
    assert parse_traceparent("garbage") == (None, None)
    assert parse_traceparent("00-" + "0" * 32 + "-00f067aa0ba902b7-01") == (None, None)


def test_init_and_capture_api(fake_ingest, monkeypatch):
    monkeypatch.setenv("PYLOGGLE_INGEST_URL", fake_ingest.url)
    monkeypatch.setenv("PYLOGGLE_API_KEY", "k")
    monkeypatch.setenv("PYLOGGLE_SERVICE", "env-svc")
    monkeypatch.setenv("PYLOGGLE_RELEASE", "9.9.9")
    client = pyloggle.init(env="qa", integrations=("logging",), flush_interval_s=0.05)
    try:
        assert pyloggle.get_client() is client
        assert client.config.ingest_url == fake_ingest.url and client.config.api_key == "k"
        assert client.config.service == "env-svc" and client.config.env == "qa" and client.config.release == "9.9.9"
        assert client.config.install_excepthooks is False

        pyloggle.set_tag("tenant", "acme")
        pyloggle.set_context("order", {"id": 7})
        pyloggle.add_breadcrumb("loading cart", category="cart", data={"items": 3})
        pyloggle.set_user({"id": "u1", "email": "u@x.y"})
        try:
            raise KeyError("missing")
        except KeyError:
            eid = pyloggle.capture_exception(tags={"area": "checkout"}, fingerprint=["checkout", "missing-key"])
        assert len(eid) == 32
        mid = pyloggle.capture_message("hello", level="warning")
        assert pyloggle.flush(5.0)
        evs = fake_ingest.wait_for(2)
        err = [e for e in evs if e["event_id"] == eid][0]
        assert err["exception"]["type"] == "KeyError" and err["mechanism"] == {"type": "capture_exception", "handled": True}
        assert err["tags"] == {"tenant": "acme", "area": "checkout"}
        assert err["extra"] == {"order": {"id": 7}}
        assert err["fingerprint"] == ["checkout", "missing-key"]
        assert err["user"]["id_hash"] and "email" not in err["user"]  # pii off by default
        assert [b["message"] for b in err["breadcrumbs"]] == ["loading cart"] and err["breadcrumbs"][0]["data"] == {"items": 3}
        assert err["release"] == "9.9.9"
        msg = [e for e in evs if e["event_id"] == mid][0]
        assert msg["level"] == "warn" and "breadcrumbs" not in msg

        @pyloggle.capture_errors(reraise=False)
        def fails():
            raise ValueError("deco")

        @pyloggle.capture_errors(reraise=True)
        async def afails():
            raise TypeError("adeco")

        assert fails() is None
        with pytest.raises(TypeError):
            asyncio.run(afails())
        pyloggle.flush(5.0)
        evs = fake_ingest.wait_for(4)
        kinds = sorted((e["exception"]["type"], e["mechanism"]["handled"]) for e in evs if e.get("exception"))
        assert kinds == [("KeyError", True), ("TypeError", False), ("ValueError", True)]
    finally:
        pyloggle.close()


def test_send_default_pii(fake_ingest, make_client):
    c = make_client(send_default_pii=True)
    pyloggle.set_user({"id": "u1", "email": "u@x.y", "username": "u"}, client=c)
    c.emit({"level": "info", "message": "x"})
    c.flush(5.0)
    ev = fake_ingest.wait_for(1)[0]
    assert ev["user"]["email"] == "u@x.y" and ev["user"]["id"] == "u1" and ev["user"]["id_hash"]


def test_init_rejects_unknown_option(fake_ingest):
    with pytest.raises(TypeError):
        pyloggle.init(fake_ingest.url, integrations=(), nope=1)
