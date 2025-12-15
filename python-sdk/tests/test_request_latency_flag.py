import asyncio


def test_fastapi_middleware_latency_is_opt_in():
    from pyloggle.client import ClientConfig
    from pyloggle.fastapi import PyloggleFastAPIMiddleware

    class FakeClient:
        def __init__(self, emit_request_latency: bool):
            self.config = ClientConfig(emit_request_latency=emit_request_latency, user_id_salt=None)
            self.emitted = []

        def emit(self, event):
            self.emitted.append(event)

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
            self.emitted.append(event)

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


def test_flask_integration_emits_request_timing_on_success_when_enabled():
    try:
        from flask import Flask
    except Exception:
        # Flask is an optional dependency for the SDK; skip if not installed.
        return

    from pyloggle.client import ClientConfig
    from pyloggle.flask import init_app

    class FakeClient:
        def __init__(self, emit_request_latency: bool):
            self.config = ClientConfig(emit_request_latency=emit_request_latency, user_id_salt=None)
            self.emitted = []

        def emit(self, event):
            self.emitted.append(event)

    def _run_once(emit_request_latency: bool):
        c = FakeClient(emit_request_latency)
        app = Flask(__name__)
        app.testing = True

        init_app(app, c)  # type: ignore[arg-type]

        @app.get("/ok")
        def _ok():
            return ("", 204)

        with app.test_client() as tc:
            resp = tc.get("/ok", headers={"User-Agent": "ua-test"})
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


def test_flask_integration_includes_latency_in_error_event_when_enabled():
    try:
        from flask import Flask
    except Exception:
        # Flask is an optional dependency for the SDK; skip if not installed.
        return

    from pyloggle.client import ClientConfig
    from pyloggle.flask import init_app

    class FakeClient:
        def __init__(self, emit_request_latency: bool):
            self.config = ClientConfig(emit_request_latency=emit_request_latency, user_id_salt=None)
            self.emitted = []

        def emit(self, event):
            self.emitted.append(event)

    def _run_once(emit_request_latency: bool):
        c = FakeClient(emit_request_latency)
        app = Flask(__name__)
        app.testing = True
        app.config["PROPAGATE_EXCEPTIONS"] = True

        init_app(app, c)  # type: ignore[arg-type]

        @app.get("/boom")
        def _boom():
            raise RuntimeError("boom")

        with app.test_client() as tc:
            try:
                tc.get("/boom")
            except RuntimeError:
                pass
        return c

    c_off = _run_once(False)
    assert len(c_off.emitted) == 1
    assert c_off.emitted[0].get("attrs") == {}

    c_on = _run_once(True)
    assert len(c_on.emitted) == 1
    assert "latency_ms" in (c_on.emitted[0].get("attrs") or {})


