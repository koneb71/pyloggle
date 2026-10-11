import asyncio
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from pyloggle import scope
from pyloggle.integrations import http as http_integration


class _H(BaseHTTPRequestHandler):
    def do_GET(self):
        status = 500 if self.path.startswith("/fail") else 404 if self.path.startswith("/missing") else 200
        self.send_response(status)
        self.send_header("Content-Type", "text/plain")
        self.end_headers()
        self.wfile.write(b"ok")

    def do_POST(self):
        n = int(self.headers.get("Content-Length", "0"))
        self.rfile.read(n)
        self.send_response(500 if self.path.startswith("/fail") else 201)
        self.end_headers()

    def log_message(self, *a):
        pass


@pytest.fixture(scope="module")
def server():
    srv = ThreadingHTTPServer(("127.0.0.1", 0), _H)
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    yield f"http://127.0.0.1:{srv.server_port}"
    srv.shutdown()
    srv.server_close()


@pytest.fixture
def crumbs():
    http_integration.install()
    token = scope.push_scope(inherit=False)
    try:
        yield scope.effective_scope().breadcrumbs
    finally:
        scope.reset_scope(token)


def _http_crumbs(crumbs):
    return [c for c in crumbs if c["category"] == "http.client"]


def test_install_is_idempotent_and_reports_libraries():
    first = http_integration.install()
    second = http_integration.install()
    assert second == []
    assert set(first) <= {"requests", "httpx", "httpx.async", "urllib3"}


def test_requests_leaves_one_crumb_without_query_or_credentials(server, crumbs):
    requests = pytest.importorskip("requests")
    r = requests.get(server + "/items?token=secret", auth=("user", "pw"), timeout=5)
    assert r.status_code == 200
    hc = _http_crumbs(crumbs)
    assert len(hc) == 1, hc  # requests goes through urllib3: still one crumb
    c = hc[0]
    assert c["type"] == "http" and c["level"] == "info" and c["message"] == f"GET {server}/items"
    assert c["data"]["method"] == "GET" and c["data"]["url"] == server + "/items" and c["data"]["status_code"] == 200
    assert "secret" not in str(c) and "pw" not in str(c) and isinstance(c["data"]["duration_ms"], float)


def test_status_levels_and_errors(server, crumbs):
    requests = pytest.importorskip("requests")
    requests.get(server + "/missing", timeout=5)
    requests.post(server + "/fail", data=b"x", timeout=5)
    with pytest.raises(Exception):
        requests.get("http://127.0.0.1:9/nothing-listens", timeout=1)
    hc = _http_crumbs(crumbs)
    assert [c["level"] for c in hc] == ["warning", "error", "error"]
    assert hc[1]["data"]["method"] == "POST" and hc[1]["data"]["status_code"] == 500
    assert "error" in hc[2]["data"] and "status_code" not in hc[2]["data"]


def test_httpx_sync_and_async(server, crumbs):
    httpx = pytest.importorskip("httpx")
    with httpx.Client() as c:
        assert c.get(server + "/a?x=1").status_code == 200

    async def go():
        async with httpx.AsyncClient() as ac:
            return (await ac.post(server + "/b", content=b"1")).status_code

    assert asyncio.run(go()) == 201
    hc = _http_crumbs(crumbs)
    assert [(c["data"]["method"], c["data"]["url"], c["data"]["status_code"]) for c in hc] == [("GET", server + "/a", 200), ("POST", server + "/b", 201)]


def test_urllib3_direct(server, crumbs):
    urllib3 = pytest.importorskip("urllib3")
    http = urllib3.PoolManager()
    r = http.request("GET", server + "/direct?q=1")
    assert r.status == 200
    hc = _http_crumbs(crumbs)
    assert len(hc) == 1 and hc[0]["data"]["url"] == server + "/direct"


def test_breadcrumbs_ride_on_error_events(server, make_client, fake_ingest):
    requests = pytest.importorskip("requests")
    import pyloggle

    client = make_client()
    with pyloggle.push_scope():
        requests.get(server + "/items", timeout=5)
        try:
            raise RuntimeError("after http")
        except RuntimeError:
            pyloggle.capture_exception(client=client)
    client.flush(2.0)
    ev = fake_ingest.wait_for(1)[0]
    cats = [b["category"] for b in ev["breadcrumbs"]]
    assert cats == ["http.client"] and ev["breadcrumbs"][0]["data"]["url"] == server + "/items"


def test_opt_out_flag_and_uninstall(server):
    import pyloggle

    http_integration.uninstall()
    try:
        prev = pyloggle.get_client()
        c = pyloggle.init(ingest_url="http://127.0.0.1:9/v1/ingest", http_breadcrumbs=False, install_excepthooks=False, integrations=("http",))
        try:
            assert http_integration._patched == {}
        finally:
            c.close()
            pyloggle._client = prev
    finally:
        http_integration.install()


def test_clean_url():
    cu = http_integration.clean_url
    assert cu("https://user:pw@api.example.com:8443/v1/x?y=1#f") == "https://api.example.com:8443/v1/x"
    assert cu("http://h/p") == "http://h/p" and cu("") == "" and cu(None) == ""
