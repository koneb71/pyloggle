"""Generate internal/model/testdata/sdk_v0.2.ndjson: representative 0.2 events (chain, locals,
breadcrumbs, request headers, user, runtime, skip_issue) that the Go decoder tests consume.

    python tests/gen_golden.py          # write
    python tests/gen_golden.py --check  # verify the committed file still has every shape
"""
from __future__ import annotations

import json
import logging
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(HERE))
OUT = os.path.join(os.path.dirname(os.path.dirname(HERE)), "internal", "model", "testdata", "sdk_v0.2.ndjson")

REQUIRED = ("chain", "vars", "breadcrumbs", "headers", "skip_issue", "runtime")

# The file is committed to a public repository: nothing about the machine that generated it.
REPO = os.path.dirname(os.path.dirname(HERE))
FORBIDDEN = ("/home/", "/Users/", "/tmp/", "0x7f", "sk_live", "C:\\Users")


def neutralize(line: str) -> str:
    """Replace machine-specific values (paths, platform, memory addresses) with fixed ones."""
    import re

    line = line.replace(REPO.rstrip("/") + "/", "/app/")
    line = re.sub(r'"platform":\s*"[^"]*"', '"platform":"Linux-x86_64"', line)
    return re.sub(r"0x[0-9a-f]{6,}", "0x000000000000", line)


def collect() -> list:
    lines = []
    lock = threading.Lock()

    class H(BaseHTTPRequestHandler):
        def do_POST(self):
            n = int(self.headers.get("Content-Length", "0"))
            body = self.rfile.read(n)
            if self.headers.get("Content-Encoding") == "gzip":
                import gzip

                body = gzip.decompress(body)
            with lock:
                lines.extend([l for l in body.decode().split("\n") if l.strip()])
            self.send_response(202)
            self.end_headers()
            self.wfile.write(b"{}")

        def log_message(self, *a):
            pass

    srv = ThreadingHTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()

    import pyloggle
    from pyloggle.logging_handler import PyloggleHandler

    client = pyloggle.init(f"http://127.0.0.1:{srv.server_port}/v1/ingest", api_key="golden", service="golden-svc", env="dev",
                           project_id="p", release="0.2.0-golden", integrations=(), flush_interval_s=0.05, gzip=False, hostname="golden-host")
    lg = logging.getLogger("golden")
    lg.handlers = []
    lg.propagate = False
    lg.setLevel(logging.DEBUG)
    lg.addHandler(PyloggleHandler(client))

    pyloggle.set_tag("team", "core")
    pyloggle.set_user({"id": "42", "email": "a@b.c"})
    lg.info("plain line")
    pyloggle.add_breadcrumb("cache miss", category="cache", data={"key": "k1"})

    def load(d):
        api_key = "not-a-real-api-key-0000"  # noqa: F841 - must be redacted in vars
        return d["k"]

    try:
        try:
            load({})
        except KeyError as e:
            raise ValueError("boom") from e
    except ValueError as exc:
        with pyloggle.push_scope():
            pyloggle.set_context("http_request", {"method": "POST", "route": "/x"})
            scope = pyloggle.scope.effective_scope()
            scope.http = {"method": "POST", "route": "/x", "url": "http://h/x?q=1", "query": "q=1", "status": 500,
                          "headers": {"user-agent": "ua", "authorization": "Bearer secret"}, "client_ip": "127.0.0.1",
                          "remote_addr": "127.0.0.1", "server_addr": "127.0.0.1:8000", "user_agent": "ua", "body": {"a": 1}}
            scope.request_id = "r1"
            eid = pyloggle.capture_exception(exc, handled=False, mechanism="flask")
            lg.error("Exception on /x [POST]", exc_info=(type(exc), exc, exc.__traceback__))  # duplicate -> skip_issue
    pyloggle.flush(5.0)
    pyloggle.close()
    srv.shutdown()
    return lines


def check(path: str) -> int:
    with open(path) as f:
        raw = [l for l in f.read().split("\n") if l.strip()]
    text = "\n".join(raw)
    missing = [k for k in REQUIRED if f'"{k}"' not in text]
    leaked = [k for k in FORBIDDEN if k in text]
    if leaked:
        print("golden file contains machine-specific values:", leaked)
        return 1
    for l in raw:
        ev = json.loads(l)
        assert ev["event_id"] and ev["ts"].endswith("Z") and ev["sdk"]["version"], l
    if missing:
        print("golden file lacks:", missing)
        return 1
    print(f"golden ok: {len(raw)} lines")
    return 0


if __name__ == "__main__":
    if "--check" in sys.argv:
        sys.exit(check(OUT))
    lines = [neutralize(l) for l in collect()]
    with open(OUT, "w") as f:
        f.write("\n".join(lines) + "\n")
    print(f"wrote {len(lines)} lines to {OUT}")
    sys.exit(check(OUT))
