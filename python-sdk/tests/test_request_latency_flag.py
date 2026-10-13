import asyncio


def test_fastapi_middleware_latency_is_opt_in():
    from pyloggle.client import ClientConfig
    from pyloggle.fastapi import PyloggleFastAPIMiddleware

    class FakeClient:
        def __init__(self, emit_request_latency: bool):
            self.config = ClientConfig(emit_request_latency=emit_request_latency, user_id_salt=None)
            self.emitted = []

        def emit(self, event):
            from pyloggle.client import Client

            self.emitted.append(Client._fit_body(self, event))  # the real client cuts bodies here

    async def app_raises(scope, receive, send):
        raise RuntimeError("boom")

    scope = {
        "type": "http",
        "method": "GET",
        "path": "/x",
        "headers": [],
        "client": ("127.0.0.1", 1234),
    }

    async def _receive():
        return {"type": "http.request"}

    async def _send(_message):
        return

    async def _run_once(emit_request_latency: bool):
        c = FakeClient(emit_request_latency)
        mw = PyloggleFastAPIMiddleware(app_raises, c)
        try:
            await mw(scope, _receive, _send)
        except RuntimeError:
            pass
        return c

    c_off = asyncio.run(_run_once(False))
    assert len(c_off.emitted) == 1
    assert c_off.emitted[0].get("attrs") == {}

    c_on = asyncio.run(_run_once(True))
    assert len(c_on.emitted) == 1
    assert "latency_ms" in (c_on.emitted[0].get("attrs") or {})


def test_fastapi_middleware_emits_request_timing_on_success_when_enabled():
    import asyncio

    from pyloggle.client import ClientConfig
    from pyloggle.fastapi import PyloggleFastAPIMiddleware

    class FakeClient:
        def __init__(self, emit_request_latency: bool):
            self.config = ClientConfig(emit_request_latency=emit_request_latency, user_id_salt=None)
            self.emitted = []

        def emit(self, event):
            from pyloggle.client import Client

            self.emitted.append(Client._fit_body(self, event))  # the real client cuts bodies here

    async def app_ok(scope, receive, send):
        await send({"type": "http.response.start", "status": 204, "headers": []})
        await send({"type": "http.response.body", "body": b""})

    scope = {
        "type": "http",
        "method": "GET",
        "path": "/ok",
        "headers": [(b"user-agent", b"ua-test")],
        "client": ("127.0.0.1", 1234),
    }

    async def _receive():
        return {"type": "http.request"}

    async def _send(_message):
        return

    async def _run_once(emit_request_latency: bool):
        c = FakeClient(emit_request_latency)
        mw = PyloggleFastAPIMiddleware(app_ok, c)
        await mw(scope, _receive, _send)
        return c

    c_off = asyncio.run(_run_once(False))
    assert c_off.emitted == []

    c_on = asyncio.run(_run_once(True))
    assert len(c_on.emitted) == 1
    ev = c_on.emitted[0]
    assert ev.get("message") == "request"
    assert (ev.get("attrs") or {}).get("latency_ms") is not None
    assert (ev.get("http") or {}).get("status") == 204


def test_fastapi_middleware_includes_url_and_body_on_success():
    import asyncio

    from pyloggle.client import ClientConfig
    from pyloggle.fastapi import PyloggleFastAPIMiddleware

    class FakeClient:
        def __init__(self, *, emit_request_body: bool, max_request_body_bytes: int):
            self.config = ClientConfig(
                emit_request_latency=True,
                emit_request_body=emit_request_body,
                max_request_body_bytes=max_request_body_bytes,
                user_id_salt=None,
            )
            self.emitted = []

        def emit(self, event):
            from pyloggle.client import Client

            self.emitted.append(Client._fit_body(self, event))  # the real client cuts bodies here

    async def app_reads_body_and_ok(scope, receive, send):
        # Ensure the middleware's receive wrapper doesn't break body consumption.
        while True:
            msg = await receive()
            if not msg:
                break
            if msg.get("type") != "http.request":
                continue
            if not msg.get("more_body"):
                break
        await send({"type": "http.response.start", "status": 204, "headers": []})
        await send({"type": "http.response.body", "body": b""})

    scope = {
        "type": "http",
        "scheme": "https",
        "server": ("example.com", 443),
        "method": "POST",
        "path": "/ok",
        "query_string": b"x=1",
        "headers": [
            (b"host", b"example.com"),
            (b"content-type", b"application/json"),
        ],
        "client": ("127.0.0.1", 1234),
    }

    raw = b'{"hello":"world"}'

    async def _run_once(*, emit_request_body: bool, max_request_body_bytes: int):
        c = FakeClient(emit_request_body=emit_request_body, max_request_body_bytes=max_request_body_bytes)
        mw = PyloggleFastAPIMiddleware(app_reads_body_and_ok, c)
        msgs = [
            {"type": "http.request", "body": raw, "more_body": False},
        ]

        async def _receive():
            if msgs:
                return msgs.pop(0)
            return {"type": "http.request", "body": b"", "more_body": False}

        async def _send(_message):
            return

        await mw(scope, _receive, _send)
        return c

    c_off = asyncio.run(_run_once(emit_request_body=False, max_request_body_bytes=4096))
    assert len(c_off.emitted) == 1
    ev = c_off.emitted[0]
    assert (ev.get("http") or {}).get("url") is not None
    assert "?x=1" in str((ev.get("http") or {}).get("url"))
    assert (ev.get("http") or {}).get("body") is None

    c_on = asyncio.run(_run_once(emit_request_body=True, max_request_body_bytes=4096))
    assert len(c_on.emitted) == 1
    ev2 = c_on.emitted[0]
    assert (ev2.get("http") or {}).get("body") == {"hello": "world"}
    assert (ev2.get("attrs") or {}).get("request_body_truncated") is None

    c_trunc = asyncio.run(_run_once(emit_request_body=True, max_request_body_bytes=8))
    assert len(c_trunc.emitted) == 1
    ev3 = c_trunc.emitted[0]
    assert (ev3.get("http") or {}).get("body") is not None
    assert (ev3.get("attrs") or {}).get("request_body_truncated") == "true"


def test_fastapi_middleware_includes_url_and_body_on_exception():
    import asyncio

    from pyloggle.client import ClientConfig
    from pyloggle.fastapi import PyloggleFastAPIMiddleware

    class FakeClient:
        def __init__(self, *, emit_request_body: bool, max_request_body_bytes: int):
            self.config = ClientConfig(
                emit_request_latency=True,
                emit_request_body=emit_request_body,
                max_request_body_bytes=max_request_body_bytes,
                user_id_salt=None,
            )
            self.emitted = []

        def emit(self, event):
            from pyloggle.client import Client

            self.emitted.append(Client._fit_body(self, event))  # the real client cuts bodies here

    async def app_reads_body_then_raises(scope, receive, send):
        await receive()
        raise RuntimeError("boom")

    scope = {
        "type": "http",
        "scheme": "http",
        "server": ("example.com", 80),
        "method": "POST",
        "path": "/boom",
        "query_string": b"y=2",
        "headers": [
            (b"host", b"example.com"),
            (b"content-type", b"text/plain"),
        ],
        "client": ("127.0.0.1", 1234),
    }

    raw = b"0123456789"

    async def _run_once(*, emit_request_body: bool, max_request_body_bytes: int):
        c = FakeClient(emit_request_body=emit_request_body, max_request_body_bytes=max_request_body_bytes)
        mw = PyloggleFastAPIMiddleware(app_reads_body_then_raises, c)
        msgs = [
            {"type": "http.request", "body": raw, "more_body": False},
        ]

        async def _receive():
            if msgs:
                return msgs.pop(0)
            return {"type": "http.request", "body": b"", "more_body": False}

        async def _send(_message):
            return

        try:
            await mw(scope, _receive, _send)
        except RuntimeError:
            pass
        return c

    c_off = asyncio.run(_run_once(emit_request_body=False, max_request_body_bytes=4096))
    assert len(c_off.emitted) == 1
    ev = c_off.emitted[0]
    assert (ev.get("http") or {}).get("url") is not None
    assert "?y=2" in str((ev.get("http") or {}).get("url"))
    assert (ev.get("http") or {}).get("body") is None

    c_on = asyncio.run(_run_once(emit_request_body=True, max_request_body_bytes=8))
    assert len(c_on.emitted) == 1
    ev2 = c_on.emitted[0]
    assert (ev2.get("http") or {}).get("body") is not None
    assert (ev2.get("attrs") or {}).get("request_body_truncated") == "true"


def test_flask_integration_emits_request_timing_on_success_when_enabled():
    try:
        from flask import Flask
    except Exception:
        # Flask is an optional dependency for the SDK; skip if not installed.
        return

    from pyloggle.client import ClientConfig
    from pyloggle.flask import init_app

    class FakeClient:
        def __init__(self, emit_request_latency: bool, *, emit_request_body: bool = False):
            self.config = ClientConfig(
                emit_request_latency=emit_request_latency,
                emit_request_body=emit_request_body,
                max_request_body_bytes=16,
                user_id_salt=None,
            )
            self.emitted = []

        def emit(self, event):
            from pyloggle.client import Client

            self.emitted.append(Client._fit_body(self, event))  # the real client cuts bodies here

    def _run_once(emit_request_latency: bool, *, emit_request_body: bool = False):
        c = FakeClient(emit_request_latency, emit_request_body=emit_request_body)
        app = Flask(__name__)
        app.testing = True

        init_app(app, c)  # type: ignore[arg-type]

        @app.post("/ok")
        def _ok():
            return ("", 204)

        with app.test_client() as tc:
            resp = tc.post(
                "/ok?x=1",
                headers={"User-Agent": "ua-test"},
                json={"hello": "world"},
            )
            assert resp.status_code == 204
        return c

    c_off = _run_once(False)
    assert c_off.emitted == []

    c_on = _run_once(True)
    assert len(c_on.emitted) == 1
    ev = c_on.emitted[0]
    assert ev.get("message") == "request"
    assert (ev.get("attrs") or {}).get("latency_ms") is not None
    assert (ev.get("http") or {}).get("status") == 204
    assert (ev.get("http") or {}).get("url") is not None
    assert "?x=1" in str((ev.get("http") or {}).get("url"))
    assert (ev.get("http") or {}).get("body") is None

    c_body = _run_once(True, emit_request_body=True)
    assert len(c_body.emitted) == 1
    ev2 = c_body.emitted[0]
    assert (ev2.get("http") or {}).get("body") is not None
    # We set a tiny max_request_body_bytes above; ensure truncation is flagged.
    assert (ev2.get("attrs") or {}).get("request_body_truncated") == "true"


def test_flask_integration_includes_latency_in_error_event_when_enabled():
    try:
        from flask import Flask
    except Exception:
        # Flask is an optional dependency for the SDK; skip if not installed.
        return

    from pyloggle.client import ClientConfig
    from pyloggle.flask import init_app

    class FakeClient:
        def __init__(self, emit_request_latency: bool, *, emit_request_body: bool = False):
            self.config = ClientConfig(
                emit_request_latency=emit_request_latency,
                emit_request_body=emit_request_body,
                max_request_body_bytes=8,
                user_id_salt=None,
            )
            self.emitted = []

        def emit(self, event):
            from pyloggle.client import Client

            self.emitted.append(Client._fit_body(self, event))  # the real client cuts bodies here

    def _run_once(emit_request_latency: bool, *, emit_request_body: bool = False):
        c = FakeClient(emit_request_latency, emit_request_body=emit_request_body)
        app = Flask(__name__)
        app.testing = True
        app.config["PROPAGATE_EXCEPTIONS"] = True

        init_app(app, c)  # type: ignore[arg-type]

        @app.post("/boom")
        def _boom():
            raise RuntimeError("boom")

        with app.test_client() as tc:
            try:
                tc.post("/boom?y=2", data="0123456789")
            except RuntimeError:
                pass
        return c

    c_off = _run_once(False)
    assert len(c_off.emitted) == 1
    assert c_off.emitted[0].get("attrs") == {}
    assert (c_off.emitted[0].get("http") or {}).get("url") is not None
    assert "?y=2" in str((c_off.emitted[0].get("http") or {}).get("url"))
    assert (c_off.emitted[0].get("http") or {}).get("body") is None

    c_on = _run_once(True)
    assert len(c_on.emitted) == 1
    assert "latency_ms" in (c_on.emitted[0].get("attrs") or {})
    assert (c_on.emitted[0].get("http") or {}).get("url") is not None
    assert "?y=2" in str((c_on.emitted[0].get("http") or {}).get("url"))
    assert (c_on.emitted[0].get("http") or {}).get("body") is None

    c_body = _run_once(True, emit_request_body=True)
    assert len(c_body.emitted) == 1
    evb = c_body.emitted[0]
    assert (evb.get("http") or {}).get("body") is not None
    assert (evb.get("attrs") or {}).get("request_body_truncated") == "true"


