import logging
import types

import pytest

django = pytest.importorskip("django")

from django.conf import settings  # noqa: E402

if not settings.configured:
    settings.configure(
        DEBUG=False,
        SECRET_KEY="test",
        ALLOWED_HOSTS=["*"],
        ROOT_URLCONF=__name__,
        MIDDLEWARE=["pyloggle.django.PyloggleMiddleware", f"{__name__}.FakeAuthMiddleware"],
        DATABASES={"default": {"ENGINE": "django.db.backends.sqlite3", "NAME": ":memory:"}},
        INSTALLED_APPS=[],
        LOGGING_CONFIG=None,
        PYLOGGLE={"service": "django-test", "env": "test", "install_excepthooks": False},
    )
    django.setup()

from asgiref.sync import async_to_sync  # noqa: E402
from django.db import connection  # noqa: E402
from django.http import Http404, JsonResponse  # noqa: E402
from django.test import AsyncClient, Client as DjangoClient  # noqa: E402
from django.urls import path  # noqa: E402

import pyloggle.django as pdj  # noqa: E402
from pyloggle import scope  # noqa: E402
from pyloggle.user import hash_user_id  # noqa: E402


class _Capture:
    def __init__(self, **cfg):
        from pyloggle import ClientConfig

        self.config = ClientConfig(**cfg)
        self.emitted = []

    def emit(self, ev):
        from pyloggle.client import Client

        self.emitted.append(Client._fit_body(self, ev))  # the real client cuts bodies here


class FakeAuthMiddleware:
    """Stands in for AuthenticationMiddleware without the auth app and its migrations."""

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        if request.headers.get("X-User") == "ada":
            request.user = types.SimpleNamespace(is_authenticated=True, pk=42, email="ada@example.com", get_username=lambda: "ada")
        else:
            request.user = types.SimpleNamespace(is_authenticated=False, pk=None)
        return self.get_response(request)


def ok(request):
    return JsonResponse({"ok": True})


def boom(request, item):
    raise RuntimeError(f"boom {item}")


def peek(request):
    sc = scope.effective_scope()
    return JsonResponse({"rid": sc.request_id, "trace": sc.trace_id, "route": sc.http["route"], "transaction": sc.transaction, "crumbs": [b["category"] for b in sc.breadcrumbs]})


def sql(request):
    with connection.cursor() as cur:
        cur.execute("SELECT 1 + 1")
        cur.fetchone()
    if request.GET.get("fail"):
        with connection.cursor() as cur:
            cur.execute("SELECT * FROM no_such_table")
    sc = scope.effective_scope()
    return JsonResponse({"crumbs": [dict(b) for b in sc.breadcrumbs if b["category"] == "db.query"]})


def not_found(request):
    raise Http404("nope")


def logged(request):
    """A view that handles its own error and logs it (tests/test_body.py)."""
    try:
        raise ValueError(request.POST.get("user"))
    except ValueError:
        logging.getLogger("shop.views").exception("handled failure")
    return JsonResponse({"ok": True})


async def aboom(request):
    raise ValueError("async boom")


async def aok(request):
    return JsonResponse({"ok": True})


urlpatterns = [
    path("ok/", ok),
    path("boom/<int:item>/", boom),
    path("peek/", peek),
    path("sql/", sql),
    path("notfound/", not_found),
    path("logged/", logged),
    path("aboom/", aboom),
    path("aok/", aok),
]


@pytest.fixture
def capture():
    c = _Capture(user_id_salt=None)
    prev = pdj._client
    pdj.init(c)
    yield c
    pdj._client = prev


def test_one_event_per_failing_request_with_route_user_and_headers(capture):
    tc = DjangoClient(raise_request_exception=False)
    r = tc.post("/boom/7/?x=1", data='{"a": 1}', content_type="application/json",
                headers={"X-Request-ID": "rid-123", "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "Authorization": "Bearer abc", "User-Agent": "ua/1", "X-User": "ada"})
    assert r.status_code == 500
    exc_events = [e for e in capture.emitted if e.get("exception")]
    assert len(exc_events) == 1
    ev = exc_events[0]
    assert ev["request_id"] == "rid-123"
    assert ev["exception"]["type"] == "RuntimeError" and ev["exception"]["message"] == "boom 7"
    assert ev["mechanism"] == {"type": "django", "handled": False}
    assert ev["http"]["method"] == "POST" and ev["http"]["route"] == "boom/<int:item>/" and ev["http"]["status"] == 500
    assert ev["http"]["query"] == "x=1" and ev["http"]["url"].endswith("/boom/7/?x=1")
    assert ev["http"]["headers"]["authorization"] == "[REDACTED]" and ev["http"]["headers"]["user-agent"] == "ua/1"
    assert ev["http"]["client_ip"] == ev["http"]["remote_addr"] == "127.0.0.1"
    assert ev["user"] == {"id_hash": hash_user_id(42, salt=None)}
    assert ev["http"]["body"] == {"a": 1}  # bodies are recorded by default
    assert scope.current_scope() is None


def test_scope_carries_identity_route_and_resets(capture):
    tc = DjangoClient()
    a = tc.get("/peek/", headers={"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}).json()
    b = tc.get("/peek/").json()
    assert a["trace"] == "4bf92f3577b34da6a3ce929d0e0e4736" and b["trace"] is None
    assert a["rid"] != b["rid"] and len(b["rid"]) == 32
    assert a["route"] == "peek/" and a["transaction"] == "peek/"
    assert a["crumbs"] == ["http.request"]
    assert scope.current_scope() is None
    assert capture.emitted == []  # no request events unless emit_request_latency


def test_request_events_carry_duration_and_status(capture):
    capture.config.emit_request_latency = True
    tc = DjangoClient(raise_request_exception=False)
    assert tc.get("/ok/").status_code == 200
    assert tc.get("/notfound/").status_code == 404
    assert tc.get("/boom/1/").status_code == 500
    reqs = [e for e in capture.emitted if e.get("message") == "request"]
    assert [e["level"] for e in reqs] == ["info", "warn"]  # the failing request has the error event instead
    assert reqs[0]["http"]["route"] == "ok/" and reqs[0]["http"]["status"] == 200
    assert isinstance(reqs[0]["duration_ms"], float) and reqs[0]["attrs"]["latency_ms"] == str(int(reqs[0]["duration_ms"]))
    err = [e for e in capture.emitted if e.get("exception")]
    assert len(err) == 1 and isinstance(err[0]["duration_ms"], float)


def test_sql_breadcrumbs_statement_only(capture):
    tc = DjangoClient(raise_request_exception=False)
    crumbs = tc.get("/sql/").json()["crumbs"]
    assert len(crumbs) == 1
    c = crumbs[0]
    assert c["category"] == "db.query" and c["type"] == "query" and c["message"] == "SELECT 1 + 1"
    assert c["data"]["db"] == "default" and isinstance(c["data"]["duration_ms"], float)
    # A failing statement leaves an error crumb and the exception is reported once.
    r = tc.get("/sql/?fail=1")
    assert r.status_code == 500
    ev = [e for e in capture.emitted if e.get("exception")]
    assert len(ev) == 1 and "no_such_table" in ev[0]["exception"]["message"]
    # Breadcrumbs are off when the flag is off.
    capture.config.db_breadcrumbs = False
    assert tc.get("/sql/").json()["crumbs"] == []


def test_handled_http_errors_are_not_issues(capture):
    capture.config.emit_request_latency = True
    tc = DjangoClient()
    assert tc.get("/notfound/").status_code == 404
    assert [e.get("exception") for e in capture.emitted] == [None]


def test_body_capture_and_pii_flag(capture):
    capture.config.emit_request_body = True
    capture.config.send_default_pii = True
    tc = DjangoClient(raise_request_exception=False)
    tc.post("/boom/2/", data='{"a": 1}', content_type="application/json", headers={"X-User": "ada"})
    ev = [e for e in capture.emitted if e.get("exception")][0]
    assert ev["http"]["body"] == {"a": 1}
    # The integration hands the raw fields to the scope; the real client decides what to send.
    assert ev["user"] == {"id_hash": hash_user_id(42, salt=None)}


def test_async_views_are_captured_once_and_scope_resets(capture):
    tc = AsyncClient(raise_request_exception=False)
    r = async_to_sync(tc.get)("/aboom/", headers={"X-Request-ID": "rid-async"})
    assert r.status_code == 500
    ev = [e for e in capture.emitted if e.get("exception")]
    assert len(ev) == 1 and ev[0]["exception"]["type"] == "ValueError" and ev[0]["request_id"] == "rid-async"
    assert ev[0]["http"]["route"] == "aboom/"
    assert async_to_sync(tc.get)("/aok/").status_code == 200
    assert scope.current_scope() is None


def test_django_request_log_line_is_marked_duplicate(make_client, fake_ingest):
    """Django logs 'Internal Server Error: /boom/1/' with exc_info after the signal; with the
    logging handler installed that line must not become a second issue, and the breadcrumbs
    (request, SQL) ride on the error event."""
    from pyloggle.logging_handler import PyloggleHandler, install_pyloggle_logging

    client = make_client(max_breadcrumbs=20)
    root = logging.getLogger()
    before = list(root.handlers)
    prev = pdj._client
    try:
        root.handlers = []
        install_pyloggle_logging(client)
        pdj.init(client)
        tc = DjangoClient(raise_request_exception=False)
        assert tc.get("/sql/?fail=1").status_code == 500
        client.flush(2.0)
    finally:
        root.handlers = [h for h in before if not isinstance(h, PyloggleHandler)]
        pdj._client = prev
    events = fake_ingest.wait_for(2)
    exc_events = [e for e in events if e.get("exception")]
    dup_events = [e for e in events if e.get("skip_issue")]
    assert len(exc_events) == 1 and exc_events[0]["mechanism"]["type"] == "django"
    assert len(dup_events) == 1 and dup_events[0]["logger"] == "django.request"
    assert dup_events[0]["attrs"]["pyloggle.duplicate_of"] == exc_events[0]["event_id"]
    cats = [b["category"] for b in exc_events[0]["breadcrumbs"]]
    assert cats[0] == "http.request" and cats.count("db.query") == 2
    assert exc_events[0]["transaction"] == "sql/"


def test_init_reads_settings_and_is_idempotent(monkeypatch):
    import pyloggle

    prev_global, prev_mod = pyloggle.get_client(), pdj._client
    pdj._client = None
    try:
        c1 = pdj.init(flush_interval_s=5.0)
        assert c1.config.service == "django-test" and c1.config.env == "test" and c1.config.flush_interval_s == 5.0
        assert pdj.init() is c1 and pdj.get_client() is c1
        c2 = pdj.init(force=True, service="other")
        assert c2 is not c1 and c2.config.service == "other"
        c2.close()
        c1.close()
    finally:
        pdj._client = prev_mod
        pyloggle._client = prev_global
