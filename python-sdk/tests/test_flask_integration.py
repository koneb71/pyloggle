import logging

import pytest

flask = pytest.importorskip("flask")

from pyloggle import scope
from pyloggle.flask import init_app


class _Capture:
    def __init__(self, **cfg):
        from pyloggle import ClientConfig

        self.config = ClientConfig(**cfg)
        self.emitted = []

    def emit(self, ev):
        self.emitted.append(ev)


def _app(client, capture_logs=False):
    app = flask.Flask(__name__)
    app.testing = True
    app.config["PROPAGATE_EXCEPTIONS"] = False  # answer 500 like production instead of re-raising
    init_app(app, client, capture_logs=capture_logs)

    @app.get("/ok")
    def ok():
        logging.getLogger("app").info("handling ok")
        return {"ok": True}

    @app.post("/boom")
    def boom():
        raise RuntimeError("boom")

    @app.get("/handled")
    def handled():
        flask.abort(404)

    return app


def test_one_event_per_failing_request_with_full_context():
    c = _Capture(user_id_salt=None, capture_request_headers=True)
    app = _app(c)
    with app.test_client() as tc:
        tc.post("/boom?x=1", headers={"X-Request-ID": "rid-123", "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "Authorization": "Bearer abc", "User-Agent": "ua/1"}, json={"a": 1})
    assert len(c.emitted) == 1
    ev = c.emitted[0]
    assert ev["request_id"] == "rid-123"
    assert ev["exception"]["type"] == "RuntimeError" and ev["mechanism"] == {"type": "flask", "handled": False}
    assert ev["http"]["method"] == "POST" and ev["http"]["route"] == "/boom" and ev["http"]["status"] == 500
    assert ev["http"]["query"] == "x=1" and "?x=1" in ev["http"]["url"]
    assert ev["http"]["headers"]["authorization"] == "[REDACTED]" and ev["http"]["headers"]["user-agent"] == "ua/1"
    assert ev["http"]["client_ip"] == ev["http"]["remote_addr"]
    assert ev["attrs"] == {}
    # scope was reset after the request
    assert scope.current_scope() is None


def test_scope_carries_trace_ids_and_breadcrumb_and_resets():
    c = _Capture(user_id_salt=None)
    app = _app(c)
    seen = {}

    @app.get("/peek")
    def peek():
        sc = scope.effective_scope()
        seen["trace"] = sc.trace_id
        seen["span"] = sc.span_id
        seen["crumbs"] = [b["category"] for b in sc.breadcrumbs]
        seen["route"] = sc.http["route"]
        return "ok"

    with app.test_client() as tc:
        tc.get("/peek", headers={"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"})
    assert seen["trace"] == "4bf92f3577b34da6a3ce929d0e0e4736" and seen["span"] == "00f067aa0ba902b7"
    assert seen["crumbs"] == ["http.request"] and seen["route"] == "/peek"
    assert scope.current_scope() is None


def test_untrusted_request_id_is_ignored():
    c = _Capture(user_id_salt=None, trust_incoming_request_id=False)
    app = _app(c)
    with app.test_client() as tc:
        tc.post("/boom", headers={"X-Request-ID": "evil"})
    assert c.emitted[0]["request_id"] != "evil" and len(c.emitted[0]["request_id"]) == 32


def test_handled_http_errors_do_not_create_events():
    c = _Capture(user_id_salt=None, emit_request_latency=True)
    app = _app(c)
    with app.test_client() as tc:
        r = tc.get("/handled")
    assert r.status_code == 404
    assert len(c.emitted) == 1 and c.emitted[0]["message"] == "request" and c.emitted[0]["level"] == "warn"


def test_logging_handler_dedupes_flask_exception():
    """Flask logs 'Exception on /boom' with exc_info in non-propagating mode; with the logging
    handler installed that record must not create a second issue."""
    c = _Capture(user_id_salt=None)
    app = flask.Flask(__name__ + "_nopropagate")
    app.testing = False
    app.config["PROPAGATE_EXCEPTIONS"] = False
    root = logging.getLogger()
    before = list(root.handlers)
    try:
        root.handlers = []
        init_app(app, c, capture_logs=True)

        @app.get("/boom")
        def boom():
            raise RuntimeError("boom")

        with app.test_client() as tc:
            r = tc.get("/boom")
        assert r.status_code == 500
    finally:
        root.handlers = before
    exc_events = [e for e in c.emitted if e.get("exception")]
    dup_events = [e for e in c.emitted if e.get("skip_issue")]
    assert len(exc_events) == 1 and exc_events[0]["mechanism"]["type"] == "flask"
    assert len(dup_events) == 1 and dup_events[0]["attrs"]["pyloggle.duplicate_of"] == exc_events[0]["event_id"]
