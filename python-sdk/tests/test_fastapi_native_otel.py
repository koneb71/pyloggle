"""FastAPI 0.142+ native OpenTelemetry: traces, metrics and exception logs without the SDK,
and the SDK + native recipe (pyloggle.fastapi.telemetry) that yields one issue per crash."""
import pytest

fastapi = pytest.importorskip("fastapi")
pytest.importorskip("opentelemetry.sdk")

try:
    from fastapi.telemetry import TelemetryConfig  # noqa: F401
except Exception:  # pragma: no cover - older FastAPI
    pytest.skip("FastAPI without native telemetry (< 0.142)", allow_module_level=True)

from fastapi.testclient import TestClient  # noqa: E402
from opentelemetry.sdk._logs import LoggerProvider  # noqa: E402
from opentelemetry.sdk._logs.export import InMemoryLogExporter, SimpleLogRecordProcessor  # noqa: E402
from opentelemetry.sdk.metrics import MeterProvider  # noqa: E402
from opentelemetry.sdk.metrics.export import InMemoryMetricReader  # noqa: E402
from opentelemetry.sdk.trace import TracerProvider  # noqa: E402
from opentelemetry.sdk.trace.export import SimpleSpanProcessor  # noqa: E402
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter  # noqa: E402
from opentelemetry.trace import SpanKind  # noqa: E402

import pyloggle.fastapi as pf  # noqa: E402
from pyloggle import otel, scope  # noqa: E402


class _Capture:
    def __init__(self, **cfg):
        from pyloggle import ClientConfig

        self.config = ClientConfig(**cfg)
        self.emitted = []

    def emit(self, ev):
        self.emitted.append(ev)


@pytest.fixture
def providers():
    spans, logs, reader = InMemorySpanExporter(), InMemoryLogExporter(), InMemoryMetricReader()
    tp = TracerProvider()
    tp.add_span_processor(SimpleSpanProcessor(spans))
    lp = LoggerProvider()
    lp.add_log_record_processor(SimpleLogRecordProcessor(logs))
    mp = MeterProvider(metric_readers=[reader])
    yield {"tracer_provider": tp, "logger_provider": lp, "meter_provider": mp}, spans, logs, reader
    tp.shutdown()
    lp.shutdown()
    mp.shutdown()


def _routes(app):
    @app.get("/hello/{name}")
    def hello(name: str):
        return {"hello": name}

    @app.get("/boom")
    def boom():
        raise RuntimeError("boom native")

    @app.get("/health")
    def health():
        return {"ok": True}


def _metric_names(reader):
    data = reader.get_metrics_data()
    names = set()
    for rm in data.resource_metrics if data else []:
        for sm in rm.scope_metrics:
            for m in sm.metrics:
                names.add(m.name)
    return names


def test_native_telemetry_alone_reports_spans_metrics_and_exception_logs(providers):
    cfg, spans, logs, reader = providers
    app = fastapi.FastAPI(telemetry={**cfg, "exclude": lambda s: s["path"] == "/health"})
    _routes(app)
    with TestClient(app, raise_server_exceptions=False) as tc:
        assert tc.get("/hello/ada").status_code == 200
        assert tc.get("/boom").status_code == 500
        assert tc.get("/health").status_code == 200
    finished = spans.get_finished_spans()
    servers = [s for s in finished if s.kind == SpanKind.SERVER]
    assert [s.attributes.get("http.route") for s in servers] == ["/hello/{name}", "/boom"]  # /health excluded
    boom = servers[1]
    assert boom.attributes["http.response.status_code"] == 500 and boom.attributes["error.type"] == "RuntimeError"
    # Operation spans (dependencies, endpoint, serialization) hang off the request span.
    children = [s for s in finished if s.parent is not None and s.parent.span_id == boom.context.span_id]
    assert len(children) >= 1
    # The unhandled exception is one log record with the OTEL exception attributes, in the trace.
    recs = [r.log_record for r in logs.get_finished_logs()]
    exc_logs = [r for r in recs if r.attributes and r.attributes.get("exception.type")]
    assert len(exc_logs) == 1
    rec = exc_logs[0]
    assert rec.attributes["exception.type"].endswith("RuntimeError") and "boom native" in rec.attributes["exception.message"]
    assert "Traceback" in rec.attributes["exception.stacktrace"] and rec.attributes["http.route"] == "/boom"
    assert rec.trace_id == boom.context.trace_id and rec.span_id == boom.context.span_id
    assert getattr(rec, "event_name", None) in (None, "http.server.request.exception")
    # HTTP metrics under the semantic-convention names the dashboards read.
    assert {"http.server.request.duration", "http.server.active_requests"} <= _metric_names(reader)


def test_sdk_plus_native_telemetry_is_one_issue_linked_to_the_trace(providers):
    cfg, spans, logs, reader = providers
    c = _Capture(user_id_salt=None, emit_request_latency=True)
    app = fastapi.FastAPI(telemetry=pf.telemetry(**cfg, exclude=lambda s: s["path"] == "/health"))
    app.add_middleware(pf.PyloggleFastAPIMiddleware, client=c, capture_logs=False)
    _routes(app)
    with TestClient(app, raise_server_exceptions=False) as tc:
        assert tc.get("/hello/ada", headers={"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}).status_code == 200
        assert tc.get("/boom").status_code == 500
        assert tc.get("/health").status_code == 200
    servers = {s.attributes.get("http.route"): s for s in spans.get_finished_spans() if s.kind == SpanKind.SERVER}
    boom = servers["/boom"]
    # The SDK owns the exception: no OTEL exception log, exactly one SDK event, carrying the
    # native span's ids, and the span is stamped with the event id.
    assert [r for r in logs.get_finished_logs() if r.log_record.attributes and r.log_record.attributes.get("exception.type")] == []
    errs = [e for e in c.emitted if e.get("exception")]
    assert len(errs) == 1
    ev = errs[0]
    assert ev["exception"]["message"] == "boom native" and ev["http"]["route"] == "/boom"
    assert boom.attributes[otel.EVENT_ID_ATTR] == ev["event_id"]
    # Request lines: the native server span yields them, so the SDK emits none for traced
    # requests; the excluded /health has no span and keeps the SDK's line.
    assert [e["http"]["route"] for e in c.emitted if e.get("message") == "request"] == ["/health"]
    # The scope took the native span's ids (same trace as the upstream traceparent, own span).
    hello = servers["/hello/{name}"]
    assert format(hello.context.trace_id, "032x") == "4bf92f3577b34da6a3ce929d0e0e4736"
    assert scope.current_scope() is None
    assert {"http.server.request.duration"} <= _metric_names(reader)


def test_scope_carries_native_span_ids(providers):
    cfg, spans, _, _ = providers
    c = _Capture(user_id_salt=None)
    app = fastapi.FastAPI(telemetry=pf.telemetry(**cfg))
    app.add_middleware(pf.PyloggleFastAPIMiddleware, client=c, capture_logs=False)

    @app.get("/peek")
    def peek():
        sc = scope.effective_scope()
        return {"trace": sc.trace_id, "span": sc.span_id}

    with TestClient(app) as tc:
        out = tc.get("/peek", headers={"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}).json()
    server = [s for s in spans.get_finished_spans() if s.kind == SpanKind.SERVER][0]
    assert out["trace"] == "4bf92f3577b34da6a3ce929d0e0e4736"
    assert out["span"] == format(server.context.span_id, "016x") != "00f067aa0ba902b7"


def test_telemetry_helper_defaults():
    assert pf.telemetry() == {"logs": False}
    cfg = pf.telemetry(logs=True, operation_spans=False)
    assert cfg == {"logs": True, "operation_spans": False}
