"""Request bodies: what each kind of body becomes, scrubbing before cutting, the default, errors
raised inside a request, and the OpenTelemetry server span."""
import contextlib
import json
import logging

import pytest

from pyloggle import ClientConfig
from pyloggle._body import PARSE_CAP, describe_body, file_note, fit_body, human_size, read_cap
from pyloggle.scrub import scrub_event

# --- what a body becomes ----------------------------------------------------------------------


def test_json_is_parsed_when_whole_and_text_when_cut():
    assert describe_body(b'{"a": 1, "b": [true]}', "application/json; charset=utf-8") == ({"a": 1, "b": [True]}, False)
    assert describe_body(b'{"a": 1, "b"', "application/json", complete=False) == ('{"a": 1, "b"', True)
    assert describe_body(b"not json", "application/vnd.api+json") == ("not json", False)


def test_url_encoded_form_becomes_a_dict_in_order_with_repeated_names_as_lists():
    body, cut = describe_body(b"user=ada&tag=a&tag=b&note=x%3Dy&empty=", "application/x-www-form-urlencoded")
    assert cut is False
    assert body == {"user": "ada", "tag": ["a", "b"], "note": "x=y", "empty": ""}
    assert list(body) == ["user", "tag", "note", "empty"]


def test_multipart_keeps_fields_and_summarizes_files():
    boundary = "XyZ"
    raw = (
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"title\"\r\n\r\nHello\r\n"
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"doc\"; filename=\"report.pdf\"\r\n"
        f"Content-Type: application/pdf\r\n\r\n%PDF-1.7 bytes\r\n"
        f"--{boundary}--\r\n"
    ).encode()
    body, cut = describe_body(raw, f"multipart/form-data; boundary={boundary}")
    assert cut is False
    assert body == {"title": "Hello", "doc": "[file report.pdf, application/pdf, 14 B]"}
    # Cut short, a multipart body is only summarized: parts may hold file bytes.
    assert describe_body(raw[:40], f"multipart/form-data; boundary={boundary}", complete=False, size=None) == (
        "[multipart/form-data body, not recorded]",
        False,
    )


def test_parsed_forms_and_uploads_from_the_framework():
    class Upload:
        def __init__(self, name, ctype, size):
            self.name, self.content_type, self.size = name, ctype, size

    body, _ = describe_body(None, "multipart/form-data", form={"title": "Hi"}, files={"photo": Upload("cat.png", "image/png", 2048)})
    assert body == {"title": "Hi", "photo": "[file cat.png, image/png, 2.0 KB]"}


def test_text_binary_and_unknown_bodies():
    assert describe_body(b"<a>1</a>", "application/xml") == ("<a>1</a>", False)
    assert describe_body(b"hello", "text/plain") == ("hello", False)
    assert describe_body(b"\x89PNG\r\n\x1a\n\x00\x00", "image/png") == ("[image/png body, 10 B, not recorded]", False)
    assert describe_body(b"\xff\xfe\x00bad", "") == ("[binary body, 6 B, not recorded]", False)
    assert describe_body(b'{"x": 1}', None) == ({"x": 1}, False)  # no content type, but plainly JSON
    assert describe_body(b"", "application/json") == (None, False)
    # Too large to read: summarized from its length alone.
    assert describe_body(None, "application/json", complete=False, size=3 * 1024 * 1024) == (
        "[application/json body, 3.0 MB, not recorded]",
        False,
    )


def test_a_character_cut_in_half_is_not_binary():
    raw = ("é" * 10).encode()[:7]  # ends inside a two-byte character
    assert describe_body(raw, "text/plain", complete=False) == ("ééé", True)


def test_sizes_and_file_notes():
    assert human_size(512) == "512 B" and human_size(1536) == "1.5 KB" and human_size(5 * 1024**3) == "5.0 GB"
    assert file_note(None, "", None) == "[file (no name)]"
    assert read_cap(ClientConfig()) == PARSE_CAP
    assert read_cap(ClientConfig(max_request_body_bytes=PARSE_CAP * 2)) == PARSE_CAP * 2


def test_fit_keeps_small_bodies_whole_and_cuts_on_a_character_boundary():
    assert fit_body({"a": 1}, 100) == ({"a": 1}, False)
    text, cut = fit_body({"note": "é" * 10}, 12)
    assert cut is True and len(text.encode()) <= 12 and text.startswith('{"note":"')
    assert fit_body("abc", 0) == (None, False)


# --- scrubbing comes before cutting ---------------------------------------------------------------


def test_secret_fields_in_body_text_are_blanked():
    def scrub(body):
        return scrub_event({"http": {"body": body}})["http"]["body"]

    assert scrub('{"user": "ada", "password": "hunter2", "api_key": "abc') == '{"user": "ada", "password": "[REDACTED]", "api_key": "[REDACTED]"'
    assert scrub("user=ada&password=hunter2&csrf_token=x1") == "user=ada&password=[REDACTED]&csrf_token=[REDACTED]"
    assert scrub('{"a": {"token": 123}, "b": [{"secret": "s"}]}') == '{"a": {"token": "[REDACTED]"}, "b": [{"secret": "[REDACTED]"}]}'


def test_payment_card_fields_are_blanked():
    body = scrub_event({"http": {"body": {"card_number": "4242 4242 4242 4242", "billingCardNumber": "4111", "cvc": "123", "amount": 5}}})["http"]["body"]
    assert body == {"card_number": "[REDACTED]", "billingCardNumber": "[REDACTED]", "cvc": "[REDACTED]", "amount": 5}


def test_a_cut_body_never_shows_a_secret(make_client, fake_ingest):
    c = make_client(max_request_body_bytes=60)
    c.emit({"message": "request", "http": {"method": "POST", "body": {"password": "hunter2-very-secret", "note": "x" * 500}}})
    c.flush()
    ev = fake_ingest.wait_for(1)[0]
    assert isinstance(ev["http"]["body"], str) and len(ev["http"]["body"].encode()) <= 60
    assert "hunter2" not in json.dumps(ev) and "[REDACTED]" in ev["http"]["body"]
    assert ev["attrs"]["request_body_truncated"] == "true"


def test_bodies_are_recorded_by_default():
    assert ClientConfig().emit_request_body is True


# --- errors raised inside a request carry its headers and body -----------------------------------


@contextlib.contextmanager
def _logger_sends_to(client, name):
    """Route one logger to this client only (the root handler may belong to another test's)."""
    from pyloggle.logging_handler import PyloggleHandler

    log = logging.getLogger(name)
    handler, propagate = PyloggleHandler(client), log.propagate
    log.addHandler(handler)
    log.propagate = False
    try:
        yield log
    finally:
        log.removeHandler(handler)
        log.propagate = propagate


def test_flask_logged_error_inside_a_view_carries_headers_and_body(make_client, fake_ingest):
    flask = pytest.importorskip("flask")
    from pyloggle.flask import init_app

    c = make_client()
    app = flask.Flask(__name__)
    init_app(app, c, capture_logs=False)
    log = logging.getLogger("shop.orders")

    @app.post("/orders")
    def orders():
        try:
            raise ValueError("no stock")
        except ValueError:
            log.exception("order failed")
        return {"ok": False}

    with _logger_sends_to(c, "shop.orders"), app.test_client() as tc:
        r = tc.post("/orders", json={"item": 3, "password": "hunter2"}, headers={"Authorization": "Bearer abcdefghij"})
        assert r.status_code == 200
    c.flush()
    ev = next(e for e in fake_ingest.wait_for(1) if e.get("message") == "order failed")
    assert ev["http"]["body"] == {"item": 3, "password": "[REDACTED]"}
    assert ev["http"]["headers"]["authorization"] == "[REDACTED]"
    assert ev["http"]["route"] == "/orders"


def test_flask_form_posts_are_recorded_as_fields(make_client, fake_ingest):
    flask = pytest.importorskip("flask")
    from pyloggle.flask import init_app

    c = make_client(emit_request_latency=True)
    app = flask.Flask(__name__)
    init_app(app, c, capture_logs=False)

    @app.post("/login")
    def login():
        return {"user": flask.request.form["user"]}  # the view parses the form first

    with app.test_client() as tc:
        assert tc.post("/login", data={"user": "ada", "password": "hunter2"}).status_code == 200
    c.flush()
    ev = fake_ingest.wait_for(1)[0]
    assert ev["http"]["body"] == {"user": "ada", "password": "[REDACTED]"}


def test_flask_large_upload_the_view_never_read_is_only_summarized(make_client, fake_ingest):
    flask = pytest.importorskip("flask")
    import io

    from pyloggle.flask import init_app

    c = make_client()
    app = flask.Flask(__name__)
    init_app(app, c, capture_logs=False)

    @app.post("/upload")
    def upload():
        raise RuntimeError("uploads are closed")  # before touching request.files

    with app.test_client() as tc:
        r = tc.post("/upload", data={"f": (io.BytesIO(b"x" * PARSE_CAP), "big.bin")}, content_type="multipart/form-data")
        assert r.status_code == 500
    c.flush()
    ev = next(e for e in fake_ingest.wait_for(1) if e.get("exception"))
    assert ev["http"]["body"].startswith("[multipart/form-data body, 64.") and ev["http"]["body"].endswith("not recorded]")


def test_asgi_captured_exception_inside_a_view_carries_the_body(make_client, fake_ingest):
    pytest.importorskip("fastapi")
    from fastapi import FastAPI, Request
    from fastapi.testclient import TestClient

    import pyloggle.fastapi as pf

    c = make_client()
    app = FastAPI()
    app.add_middleware(pf.PyloggleFastAPIMiddleware, client=c, capture_logs=False)

    @app.post("/pay")
    async def pay(request: Request):
        await request.json()
        try:
            raise RuntimeError("card declined")
        except RuntimeError as e:
            c.emit({"level": "error", "message": str(e)})
        return {"ok": False}

    with TestClient(app) as tc:
        assert tc.post("/pay", json={"amount": 5, "card_token": "tok_123"}).status_code == 200
    c.flush()
    ev = fake_ingest.wait_for(1)[0]
    assert ev["http"]["body"] == {"amount": 5, "card_token": "[REDACTED]"}
    assert ev["http"]["route"] == "/pay"


def test_django_logged_error_inside_a_view_carries_the_body(make_client, fake_ingest):
    pytest.importorskip("django")
    from tests import test_django_integration as tdj  # configures Django with its URLs

    import pyloggle.django as pdj

    c = make_client()
    prev = pdj._client
    pdj.init(c)
    try:
        with _logger_sends_to(c, "shop.views"):
            r = tdj.DjangoClient().post("/logged/", data={"user": "ada", "password": "hunter2"})
        assert r.status_code == 200
    finally:
        pdj._client = prev
    c.flush()
    ev = next(e for e in fake_ingest.wait_for(1) if e.get("message") == "handled failure")
    assert ev["http"]["body"] == {"user": "ada", "password": "[REDACTED]"}


# --- OpenTelemetry: the server span carries the body ---------------------------------------------


def test_fastapi_server_span_carries_the_scrubbed_body(make_client):
    fastapi = pytest.importorskip("fastapi")
    pytest.importorskip("opentelemetry.sdk")
    try:
        from fastapi.telemetry import TelemetryConfig  # noqa: F401
    except Exception:
        pytest.skip("FastAPI without native telemetry (< 0.142)")
    from fastapi.testclient import TestClient
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import SimpleSpanProcessor
    from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
    from opentelemetry.trace import SpanKind

    import pyloggle.fastapi as pf

    spans = InMemorySpanExporter()
    tp = TracerProvider()
    tp.add_span_processor(SimpleSpanProcessor(spans))
    c = make_client(max_request_body_bytes=4096)
    app = fastapi.FastAPI(telemetry=pf.telemetry(tracer_provider=tp))
    app.add_middleware(pf.PyloggleFastAPIMiddleware, client=c, capture_logs=False)

    @app.post("/signup")
    async def signup(payload: dict):
        return {"ok": True}

    try:
        with TestClient(app) as tc:
            assert tc.post("/signup", json={"email": "ada@example.com", "password": "hunter2"}).status_code == 200
    finally:
        tp.shutdown()
    server = next(s for s in spans.get_finished_spans() if s.kind == SpanKind.SERVER)
    assert json.loads(server.attributes["http.request.body"]) == {"email": "ada@example.com", "password": "[REDACTED]"}
    assert "http.request.body.truncated" not in server.attributes
