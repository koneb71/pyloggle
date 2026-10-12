#!/usr/bin/env python3
"""Fill a Pyloggle server with two days of realistic demo data.

    python3 scripts/demo_data.py                                  # http://127.0.0.1:8080, sign-in off
    python3 scripts/demo_data.py --url https://logs.example.com --key pl_...

What it sends, all made up (apps, people and addresses use example names):
  * web requests from five apps over two days, with versions, a slow and a failing endpoint;
  * errors with full reports: one that jumps today, one that is new since a release, a steady
    old one, and one that is resolved and then comes back;
  * background-job messages from a worker that went quiet two hours ago;
  * request timelines through OpenTelemetry (checkout -> inventory -> worker) for the last hour;
  * OpenTelemetry metrics: FastAPI's request duration, active requests, CPU and memory.

Without --key it uses the default project, which works while sign-in is off. With --key the
key needs Send data, and Make changes to resolve the issue that comes back. Uses only the
standard library; the same --seed gives the same data, shifted to the current time.
"""
import argparse
import json
import random
import sys
import time
import urllib.error
import urllib.request

H = 3600


class Server:
    def __init__(self, url, key):
        self.url = url.rstrip("/")
        self.key = key

    def _headers(self, content_type):
        h = {"Content-Type": content_type, "X-Pyloggle-Request": "1"}
        if self.key:
            h["X-API-Key"] = self.key
        return h

    def post(self, path, body, content_type="application/json"):
        req = urllib.request.Request(self.url + path, data=body, headers=self._headers(content_type), method="POST")
        with urllib.request.urlopen(req, timeout=120) as r:
            return json.loads(r.read() or b"{}")

    def get(self, path):
        req = urllib.request.Request(self.url + path, headers=self._headers("application/json"))
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.loads(r.read())

    @property
    def api(self):
        # Access keys use the query API; the app's own API works while sign-in is off.
        return "/v1/q" if self.key else "/ui/api"

    def send(self, events):
        # Small, paced batches: each app has its own queue on the server (2,000 events by
        # default) and a burst bigger than that is dropped, as it would be from a real app.
        dropped = {}
        for i in range(0, len(events), 500):
            chunk = events[i : i + 500]
            res = self.post("/v1/ingest", "\n".join(json.dumps(e) for e in chunk).encode(), "application/x-ndjson")
            if res.get("rejected"):
                print("  rejected:", res.get("errors"), file=sys.stderr)
            for reason, n in (res.get("dropped") or {}).items():
                dropped[reason] = dropped.get(reason, 0) + n
            time.sleep(0.15 if (res.get("dropped") or {}).get("queue_full") is None else 1.5)
        if dropped:
            print("  the server dropped:", dropped, file=sys.stderr)


def iso(t):
    return time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(t)) + ".%03dZ" % int((t % 1) * 1000)


def hexid(n):
    return "".join(random.choice("0123456789abcdef") for _ in range(n))


# --- web requests ------------------------------------------------------------------------------

# app: environment, [(method, route, typical ms)], requests a minute, failed share (day before,
# today), [(version, released hours ago)]
APPS = {
    "web": ("production", [("GET", "/", 40), ("GET", "/products/{id}", 60), ("GET", "/cart", 35)], 6, 0.002, 0.002, [("3.0.4", 60), ("3.1.0", 36)]),
    "api": ("production", [("GET", "/v1/orders", 45), ("POST", "/v1/orders", 120), ("GET", "/v1/stock/{sku}", 25)], 8, 0.004, 0.018, [("1.8.1", 60), ("1.8.2", 30)]),
    "billing": ("production", [("POST", "/charge", 210), ("POST", "/refund", 160)], 2, 0.006, 0.006, [("2.2.0", 40), ("2.3.0", 3)]),
    "search": ("staging", [("GET", "/search", 90)], 1, 0.0, 0.0, [("0.9.0", 50)]),
}


def version_at(versions, t, now):
    current = versions[0][0]
    for name, since in versions:
        if t >= now - since * H:
            current = name
    return current


def requests(now, days):
    out = []
    for app, (env, routes, per_minute, failed_before, failed_today, versions) in APPS.items():
        minutes = days * 24 * 60
        for m in range(minutes):
            minute = now - (minutes - m) * 60
            share = failed_today if minute > now - 24 * H else failed_before
            if app == "billing" and minute > now - 3 * H:
                share = 0.09  # version 2.3.0 breaks charging
            for _ in range(max(0, int(random.gauss(per_minute, per_minute * 0.25)))):
                t = minute + random.random() * 60
                method, route, typical = random.choice(routes)
                failed = random.random() < share
                status = 500 if failed else (404 if random.random() < 0.01 else 200)
                ms = max(1, int(random.lognormvariate(0, 0.45) * typical * (2.2 if failed else 1)))
                line = {
                    "ts": iso(t), "service": app, "env": env, "release": version_at(versions, t, now),
                    "level": "error" if failed else "info", "logger": f"{app}.http",
                    "message": f"{method} {route} failed" if failed else "request",
                    "hostname": f"{app}-{random.randint(1, 3)}", "request_id": "req-" + hexid(12),
                    "http": {"method": method, "route": route, "status": status}, "duration_ms": ms,
                    "skip_issue": True,
                }
                if random.random() < 0.6:
                    line["user"] = {"id_hash": f"user-{random.randint(1, 300)}"}
                out.append(line)
    return out


# --- errors with full reports ------------------------------------------------------------------

STEPS = {
    "api": [("query", "SELECT id, total FROM orders WHERE customer_id = %s"), ("http", "GET https://stock.internal.example/v1/levels -> 200"), ("log", "loading orders for customer")],
    "billing": [("log", "authorizing card for order 81723"), ("http", "POST https://cards.example.net/authorize -> timeout"), ("log", "retrying card network")],
    "web": [("navigation", "GET /products/1042"), ("query", "SELECT * FROM products WHERE id = %s"), ("log", "rendering product page")],
}


MODULES = {"OperationalError": "psycopg", "KeyError": "builtins", "TemplateNotFound": "jinja2.exceptions", "CardNetworkTimeout": "app.payments"}


def error(t, app, env, version, kind, message, fn, file, line, code, local_vars, route=None, method="GET", user=True):
    lib = {
        "filename": "site-packages/psycopg/cursor.py", "abs_path": "/usr/lib/python3.12/site-packages/psycopg/cursor.py",
        "module": "psycopg.cursor", "function": "execute", "lineno": 732, "context": "raise ex.with_traceback(None)", "in_app": False,
    }
    mine = {
        "filename": f"app/{file}", "abs_path": f"/srv/app/{file}", "module": f"app.{file[:-3]}", "function": fn, "lineno": line,
        "context": code, "pre_context": ["", f"def {fn}(request):", "    params = parse(request)"], "post_context": ["    return render(rows)", ""],
        "vars": local_vars, "in_app": True,
    }
    frames = [mine, lib] if app == "api" else [mine]
    ev = {
        "ts": iso(t), "service": app, "env": env, "release": version, "level": "error", "logger": f"app.{app}", "message": message,
        "hostname": f"{app}-{random.randint(1, 3)}", "request_id": "req-" + hexid(12),
        "exception": {"type": kind, "module": MODULES.get(kind, "app.errors"), "message": message, "frames": frames},
        "mechanism": {"type": "asgi" if app == "api" else "flask", "handled": False},
        "runtime": {"name": "CPython", "version": "3.12.4", "platform": "Linux-x86_64"},
        "sdk": {"name": "pyloggle-python", "version": "1.0.0"},
        "tags": {"region": random.choice(["eu-west", "us-east"])},
        "breadcrumbs": [
            {"ts": iso(t - 0.3 + i * 0.1), "type": kind_, "category": kind_, "level": "info", "message": msg}
            for i, (kind_, msg) in enumerate(STEPS.get(app, []))
        ],
    }
    if user:
        ev["user"] = {"id_hash": f"user-{random.randint(1, 300)}"}
    if route:
        ev["http"] = {"method": method, "route": route, "status": 500, "url": f"https://{app}.example.com{route.replace('{id}', '1042')}"}
        ev["duration_ms"] = random.randint(80, 2400)
    return ev


def errors(now):
    out = []
    api = APPS["api"][5]
    # The database times out much more often today: a jump.
    for i in range(105):
        t = now - 24 * H - random.random() * 24 * H if i < 15 else now - random.random() * 24 * H
        out.append(error(t, "api", "production", version_at(api, t, now), "OperationalError", "canceling statement due to statement timeout",
                         "list_orders", "orders.py", 54, "rows = db.execute(QUERY, params)", {"customer_id": "8812", "limit": "50"}, "/v1/orders"))
    # New since billing 2.3.0, three hours ago.
    for _ in range(46):
        t = now - random.random() * 3 * H
        out.append(error(t, "billing", "production", "2.3.0", "CardNetworkTimeout", "card network did not answer within 10s", "charge",
                         "payments.py", 131, "resp = network.authorize(card, amount, timeout=10)", {"amount": "49.90", "currency": "'EUR'"}, "/charge", "POST"))
    # A steady old problem.
    web = APPS["web"][5]
    for _ in range(24):
        t = now - random.random() * 48 * H
        out.append(error(t, "web", "production", version_at(web, t, now), "KeyError", "'currency'", "render_price", "pricing.py", 22,
                         "symbol = SYMBOLS[product['currency']]", {"product": "{'id': 1042, 'price': 19.0}"}, "/products/{id}"))
    return out


def receipt_error(t):
    return error(t, "web", "production", "3.1.0", "TemplateNotFound", "emails/receipt.html", "send_receipt", "mailer.py", 77,
                 "tpl = env.get_template('emails/receipt.html')", {"order_id": "81723"}, user=False)


def worker_messages(now, days):
    out = []
    for m in range(0, days * 24 * 60 - 120, 2):  # quiet for the last two hours
        t = now - days * 24 * H + m * 60 + random.random() * 60
        out.append({"ts": iso(t), "service": "worker", "env": "production", "release": "5.2.0", "level": "info", "logger": "worker.jobs",
                    "message": random.choice(["sent order confirmation", "rebuilt search index", "synced stock levels"]),
                    "attrs": {"job_id": hexid(8), "queue": random.choice(["emails", "search", "stock"])}})
        if random.random() < 0.03:
            out.append({"ts": iso(t + 1), "service": "worker", "env": "production", "release": "5.2.0", "level": "warn", "logger": "worker.jobs",
                        "message": "stock sync took longer than 30s", "attrs": {"queue": "stock"}})
    return out


# --- OpenTelemetry: request timelines and metrics -------------------------------------------------


def kv(k, v):
    if isinstance(v, bool):
        return {"key": k, "value": {"boolValue": v}}
    if isinstance(v, int):
        return {"key": k, "value": {"intValue": str(v)}}
    return {"key": k, "value": {"stringValue": str(v)}}


def span(trace, sid, parent, name, kind, start, ms, attrs, status=0, message="", events=None):
    s = {"traceId": trace, "spanId": sid, "name": name, "kind": kind, "startTimeUnixNano": str(int(start * 1e9)),
         "endTimeUnixNano": str(int((start + ms / 1000) * 1e9)), "attributes": [kv(k, v) for k, v in attrs.items()],
         "status": {"code": status, **({"message": message} if message else {})}}
    if parent:
        s["parentSpanId"] = parent
    if events:
        s["events"] = events
    return s


def resource(app):
    return {"attributes": [kv("service.name", app), kv("deployment.environment", "production"), kv("service.version", "1.4.0"),
                           kv("host.name", f"{app}-1"), kv("telemetry.sdk.language", "python")]}


TRACEBACK = (
    "Traceback (most recent call last):\n"
    '  File "/srv/app/checkout.py", line 51, in place_order\n    reserve(items)\n'
    '  File "/srv/app/inventory.py", line 17, in reserve\n    raise RuntimeError(\'inventory unavailable\')\n'
    "RuntimeError: inventory unavailable\n"
)


def timelines(now, count=150):
    checkout, inventory, worker, messages = [], [], [], []
    for i in range(count):
        t0 = now - random.random() * H
        trace, root = hexid(32), hexid(16)
        method, route = random.choice([("POST", "/orders"), ("GET", "/orders/{id}"), ("POST", "/pay"), ("GET", "/cart")])
        slow, failed = random.random() < 0.15, random.random() < 0.08
        total = (900 if slow else 120) * random.uniform(0.7, 1.4)
        events = None
        if failed:
            events = [{"timeUnixNano": str(int((t0 + total / 1000 * 0.9) * 1e9)), "name": "exception",
                       "attributes": [kv("exception.type", "RuntimeError"), kv("exception.message", "inventory unavailable"),
                                      kv("exception.escaped", "true"), kv("exception.stacktrace", TRACEBACK)]}]
        status_code = 500 if failed else (201 if method == "POST" else 200)
        checkout.append(span(trace, root, None, f"{method} {route}", 2, t0, total,
                             {"http.request.method": method, "http.route": route, "http.response.status_code": status_code,
                              "url.path": route.replace("{id}", "4711")}, 2 if failed else 1, "RuntimeError" if failed else "", events))
        checkout.append(span(trace, hexid(16), root, "fastapi.dependencies", 1, t0 + 0.002, 3, {}))
        checkout.append(span(trace, hexid(16), root, "SELECT shop.orders", 3, t0 + 0.006, total * 0.18,
                             {"db.system": "postgresql", "db.name": "shop", "db.statement": "SELECT * FROM orders WHERE id = $1"}))
        call, call_start = hexid(16), t0 + 0.006 + total * 0.19 / 1000
        call_ms = total * (0.55 if slow else 0.35)
        checkout.append(span(trace, call, root, "GET /stock/{sku}", 3, call_start, call_ms,
                             {"http.request.method": "GET", "http.response.status_code": 503 if failed else 200, "server.address": "inventory"},
                             2 if failed else 0, "503" if failed else ""))
        inner = hexid(16)
        inventory.append(span(trace, inner, call, "GET /stock/{sku}", 2, call_start + 0.002, call_ms - 4,
                              {"http.request.method": "GET", "http.route": "/stock/{sku}", "http.response.status_code": 503 if failed else 200},
                              2 if failed else 1, "upstream" if failed else ""))
        inventory.append(span(trace, hexid(16), inner, "GET redis", 3, call_start + 0.003, 2, {"db.system": "redis", "db.statement": "GET sku:9"}))
        if slow:
            inventory.append(span(trace, hexid(16), inner, "SELECT inventory.stock", 3, call_start + 0.006, call_ms - 12,
                                  {"db.system": "postgresql", "db.statement": "SELECT stock FROM stock WHERE sku = $1 FOR UPDATE"}))
        if not failed:
            checkout.append(span(trace, hexid(16), root, "fastapi.serialize", 1, t0 + total * 0.92 / 1000, total * 0.05, {}))
            worker.append(span(trace, hexid(16), root, "send order_confirmation", 4, t0 + total * 0.95 / 1000, 4,
                               {"messaging.system": "rabbitmq", "messaging.destination.name": "emails"}))
        for j, text in enumerate(["order received", "reserving stock", "order failed" if failed else "order placed"]):
            messages.append({"ts": iso(t0 + 0.01 * (j + 1)), "service": "checkout", "env": "production", "release": "1.4.0",
                             "level": "error" if failed and j == 2 else "info", "logger": "app.checkout", "message": text,
                             "trace_id": trace, "span_id": root, "attrs": {"order_id": str(80000 + i)}, "skip_issue": failed and j == 2})
    body = {"resourceSpans": [
        {"resource": resource("checkout"), "scopeSpans": [{"scope": {"name": "fastapi"}, "spans": checkout}]},
        {"resource": resource("inventory"), "scopeSpans": [{"scope": {"name": "opentelemetry.instrumentation.flask"}, "spans": inventory}]},
        {"resource": resource("worker"), "scopeSpans": [{"scope": {"name": "celery"}, "spans": worker}]},
    ]}
    return body, messages


BOUNDS = [0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10]


def metrics(now, minutes=90):
    start = int(now) - minutes * 60
    routes = [("GET", "/orders/{id}", 200, 0.02, 12), ("POST", "/orders", 201, 0.08, 6), ("POST", "/pay", 200, 0.15, 4),
              ("POST", "/pay", 500, 0.9, 1), ("GET", "/cart", 200, 0.03, 9)]
    totals = [{"count": 0, "sum": 0.0, "buckets": [0] * (len(BOUNDS) + 1)} for _ in routes]
    duration, active, cpu_points, memory = [], [], [], []
    cpu = 100.0
    for m in range(minutes + 1):
        ts = str((start + m * 60) * 10**9)
        for s, (method, route, status, typical, per_minute) in zip(totals, routes):
            for _ in range(max(0, int(random.gauss(per_minute, per_minute / 3)))):
                v = max(0.0005, random.lognormvariate(0, 0.45) * typical * (1.6 if 40 <= m <= 55 else 1))
                s["buckets"][next((k for k, b in enumerate(BOUNDS) if v <= b), len(BOUNDS))] += 1
                s["count"] += 1
                s["sum"] += v
            duration.append({"attributes": [kv("http.request.method", method), kv("http.route", route), kv("http.response.status_code", status)],
                             "startTimeUnixNano": str(start * 10**9), "timeUnixNano": ts, "count": str(s["count"]), "sum": s["sum"],
                             "bucketCounts": [str(x) for x in s["buckets"]], "explicitBounds": BOUNDS})
        active.append({"attributes": [kv("http.request.method", "GET")], "timeUnixNano": ts, "asInt": str(random.randint(0, 6))})
        cpu += random.uniform(20, 40)
        cpu_points.append({"attributes": [kv("cpu.mode", "user")], "startTimeUnixNano": str(start * 10**9), "timeUnixNano": ts, "asDouble": cpu})
        for host, base in (("checkout-1", 380), ("checkout-2", 420)):
            memory.append({"attributes": [kv("host.name", host)], "timeUnixNano": ts, "asInt": str(int(base * 1048576 + m * 300000 + random.randint(-2000000, 2000000)))})
    return {"resourceMetrics": [{
        "resource": resource("checkout"),
        "scopeMetrics": [
            {"scope": {"name": "fastapi", "version": "0.143.0"}, "metrics": [
                {"name": "http.server.request.duration", "unit": "s", "description": "Duration of HTTP server requests.",
                 "histogram": {"aggregationTemporality": 2, "dataPoints": duration}},
                {"name": "http.server.active_requests", "unit": "{request}", "sum": {"aggregationTemporality": 2, "isMonotonic": False, "dataPoints": active}},
            ]},
            {"scope": {"name": "opentelemetry.instrumentation.system_metrics"}, "metrics": [
                {"name": "process.cpu.time", "unit": "s", "sum": {"aggregationTemporality": 2, "isMonotonic": True, "dataPoints": cpu_points}},
                {"name": "process.memory.usage", "unit": "By", "gauge": {"dataPoints": memory}},
            ]},
        ],
    }]}


def main():
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--url", default="http://127.0.0.1:8080", help="the Pyloggle server (default %(default)s)")
    p.add_argument("--key", default="", help="an access key; without one the default project is used")
    p.add_argument("--days", type=int, default=2, help="days of web requests and messages (default %(default)s)")
    p.add_argument("--seed", type=int, default=11, help="random seed (default %(default)s)")
    args = p.parse_args()
    random.seed(args.seed)
    server = Server(args.url, args.key)
    now = time.time()

    try:
        print("web requests and messages")
        server.send(requests(now, args.days) + worker_messages(now, args.days))
        print("errors")
        server.send(errors(now))
        # A problem from yesterday that is resolved now and comes back in the last hour.
        server.send([receipt_error(now - 30 * H - random.random() * 4 * H) for _ in range(12)])
        time.sleep(5)
        for issue in server.get(server.api + "/issues?status=all&from=-3d&q=TemplateNotFound&limit=5")["items"]:
            server.post(f"{server.api}/issues/{issue['issue_id']}/state", json.dumps({"status": "resolved"}).encode())
        time.sleep(2)
        server.send([receipt_error(now - random.random() * 50 * 60) for _ in range(5)])
        print("request timelines")
        spans, messages = timelines(now)
        server.post("/v1/traces", json.dumps(spans).encode())
        server.send(messages)
        print("metrics")
        server.post("/v1/metrics", json.dumps(metrics(now)).encode())
    except urllib.error.HTTPError as e:
        sys.exit(f"{e.code} from {e.url}: {e.read()[:300].decode(errors='replace')}")
    except urllib.error.URLError as e:
        sys.exit(f"cannot reach {args.url}: {e.reason}")
    print("done: open", args.url + "/ui/")


if __name__ == "__main__":
    main()
