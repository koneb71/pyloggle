import gzip
import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest


class FakeIngest:
    """In-process ingest server recording every NDJSON batch; statuses can be scripted. Request
    counts (POST .../requests) are kept apart from events."""

    def __init__(self):
        self.requests = []  # list of dicts: headers, lines, raw_len
        self.statuses = []  # queue of status codes to answer with (default 202)
        self.lock = threading.Lock()
        srv = self

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("Content-Length", "0"))
                raw = self.rfile.read(n)
                body = raw
                if self.headers.get("Content-Encoding") == "gzip":
                    body = gzip.decompress(raw)
                lines = [json.loads(l) for l in body.decode("utf-8").split("\n") if l.strip()]
                with srv.lock:
                    status = srv.statuses.pop(0) if srv.statuses else 202
                    hdrs = {k.lower(): v for k, v in self.headers.items()}
                    srv.requests.append({"headers": hdrs, "lines": lines, "raw_len": len(raw), "status": status, "path": self.path})
                self.send_response(status)
                if status == 429:
                    self.send_header("Retry-After", "0")
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"accepted":%d}' % len(lines))

            def log_message(self, *a):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), H)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = f"http://127.0.0.1:{self.server.server_port}/v1/ingest"

    @property
    def events(self):
        """Events the server accepted (batches answered with a non-2xx status do not count)."""
        with self.lock:
            return [ev for r in self.requests if 200 <= r["status"] < 300 and not r["path"].endswith("/requests") for ev in r["lines"]]

    @property
    def counts(self):
        """Request-count lines the server accepted."""
        with self.lock:
            return [c for r in self.requests if 200 <= r["status"] < 300 and r["path"].endswith("/requests") for c in r["lines"]]

    def wait_for(self, n, timeout=5.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if len(self.events) >= n:
                return self.events
            time.sleep(0.02)
        return self.events

    def close(self):
        self.server.shutdown()
        self.server.server_close()


@pytest.fixture
def fake_ingest():
    srv = FakeIngest()
    yield srv
    srv.close()


@pytest.fixture
def make_client(fake_ingest):
    from pyloggle import Client, ClientConfig

    created = []

    def _make(**overrides):
        cfg = dict(ingest_url=fake_ingest.url, api_key="test-key", service="svc", env="test", project_id="p",
                   flush_interval_s=0.05, request_timeout_s=2.0, install_excepthooks=False, retry_backoff_s=0.01)
        cfg.update(overrides)
        c = Client(ClientConfig(**cfg))
        created.append(c)
        return c

    yield _make
    for c in created:
        c.close()


@pytest.fixture(autouse=True)
def _clean_scope():
    from pyloggle import scope

    scope.clear_scope()
    g = scope.global_scope()
    g.tags.clear()
    g.extra.clear()
    g.user = None
    g.request_id = g.trace_id = g.span_id = None
    g.breadcrumbs.clear()
    yield
    scope.clear_scope()
