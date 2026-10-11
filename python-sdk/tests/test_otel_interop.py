import pytest

otel_sdk = pytest.importorskip("opentelemetry.sdk")

from opentelemetry import trace  # noqa: E402
from opentelemetry.sdk.trace import TracerProvider  # noqa: E402
from opentelemetry.sdk.trace.export import SimpleSpanProcessor  # noqa: E402
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter  # noqa: E402

import pyloggle  # noqa: E402
from pyloggle import otel, scope  # noqa: E402
from pyloggle.stack import captured_event_id, mark_captured  # noqa: E402


@pytest.fixture
def tracer():
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    yield provider.get_tracer("test"), exporter
    provider.shutdown()


def test_event_ids_follow_the_active_span(tracer, make_client, fake_ingest):
    tr, exporter = tracer
    client = make_client()
    assert otel.current_ids() == (None, None)
    with tr.start_as_current_span("work") as span, pyloggle.push_scope():
        sc = scope.effective_scope()
        sc.trace_id, sc.span_id = "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"  # the upstream parent ids
        tid, sid = otel.current_ids()
        ctx = span.get_span_context()
        assert tid == format(ctx.trace_id, "032x") and sid == format(ctx.span_id, "016x")
        client.emit({"message": "inside"})
        try:
            raise ValueError("boom")
        except ValueError:
            event_id = pyloggle.capture_exception(client=client)
    client.emit({"message": "outside"})
    client.flush(2.0)
    events = {e["message"]: e for e in fake_ingest.wait_for(3)}
    assert events["inside"]["trace_id"] == tid and events["inside"]["span_id"] == sid
    assert events["boom"]["trace_id"] == tid and events["boom"]["span_id"] == sid
    assert "trace_id" not in events["outside"] or events["outside"]["trace_id"] in ("", None)
    # The captured exception stamped the span with its event id.
    spans = exporter.get_finished_spans()
    assert len(spans) == 1 and spans[0].attributes[otel.EVENT_ID_ATTR] == event_id


def test_explicit_ids_and_scope_fallback(make_client, fake_ingest):
    client = make_client()
    with pyloggle.push_scope():
        sc = scope.effective_scope()
        sc.trace_id, sc.span_id = "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"
        client.emit({"message": "scoped"})
        client.emit({"message": "explicit", "trace_id": "ffff2f3577b34da6a3ce929d0e0e4736", "span_id": "1111aa0ba902b7ff"})
    client.flush(2.0)
    events = {e["message"]: e for e in fake_ingest.wait_for(2)}
    assert events["scoped"]["trace_id"] == "4bf92f3577b34da6a3ce929d0e0e4736" and events["scoped"]["span_id"] == "00f067aa0ba902b7"
    assert events["explicit"]["trace_id"] == "ffff2f3577b34da6a3ce929d0e0e4736" and events["explicit"]["span_id"] == "1111aa0ba902b7ff"


def test_server_span_detection(tracer):
    tr, _ = tracer
    assert otel.server_span_active() is False
    with tr.start_as_current_span("client", kind=trace.SpanKind.CLIENT):
        assert otel.server_span_active() is False
    with tr.start_as_current_span("GET", kind=trace.SpanKind.SERVER):
        assert otel.server_span_active() is True


def test_mark_captured_without_a_span_is_fine():
    exc = RuntimeError("x")
    mark_captured(exc, "ev-1")
    assert captured_event_id(exc) == "ev-1"
    assert otel.annotate_captured("ev-1") is False


def test_otel_owns_exceptions_marks_but_does_not_report():
    flask = pytest.importorskip("flask")
    from pyloggle.flask import init_app

    class _Capture:
        def __init__(self):
            self.config = pyloggle.ClientConfig(otel_owns_exceptions=True, user_id_salt=None)
            self.emitted = []

        def emit(self, ev):
            self.emitted.append(ev)

    c = _Capture()
    app = flask.Flask(__name__)
    app.config["PROPAGATE_EXCEPTIONS"] = False
    init_app(app, c, capture_logs=False)
    seen = {}

    @app.get("/boom")
    def boom():
        exc = RuntimeError("boom")
        seen["exc"] = exc
        raise exc

    with app.test_client() as tc:
        assert tc.get("/boom").status_code == 500
    assert c.emitted == []  # the OTLP path reports it
    assert captured_event_id(seen["exc"]) == "otel"  # the framework's log line stays a duplicate
