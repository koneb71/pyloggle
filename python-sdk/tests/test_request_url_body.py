import asyncio
import unittest


class _CaptureClient:
    def __init__(self, config):
        self.config = config
        self.emitted = []

    def emit(self, event):
        self.emitted.append(event)


class TestRequestUrlAndBody(unittest.TestCase):
    def test_fastapi_success_includes_http_url(self) -> None:
        from pyloggle.client import ClientConfig
        from pyloggle.fastapi import PyloggleFastAPIMiddleware

        async def app_ok(scope, receive, send):
            await send({"type": "http.response.start", "status": 204, "headers": []})
            await send({"type": "http.response.body", "body": b""})

        scope = {
            "type": "http",
            "scheme": "https",
            "server": ("example.com", 443),
            "method": "GET",
            "path": "/ok",
            "query_string": b"x=1",
            "headers": [(b"host", b"example.com")],
            "client": ("127.0.0.1", 1234),
        }

        async def _receive():
            return {"type": "http.request", "body": b"", "more_body": False}

        async def _send(_message):
            return

        c = _CaptureClient(ClientConfig(emit_request_latency=True, user_id_salt=None))
        mw = PyloggleFastAPIMiddleware(app_ok, c)  # type: ignore[arg-type]
        asyncio.run(mw(scope, _receive, _send))

        self.assertEqual(len(c.emitted), 1)
        ev = c.emitted[0]
        self.assertEqual(ev.get("message"), "request")
        self.assertEqual((ev.get("http") or {}).get("url"), "https://example.com/ok?x=1")
        # Ensure both client (end-user) and server address are emitted.
        self.assertEqual((ev.get("http") or {}).get("remote_addr"), "127.0.0.1")
        self.assertEqual((ev.get("http") or {}).get("server_addr"), "example.com:443")
        self.assertNotIn("body", (ev.get("http") or {}))

    def test_fastapi_body_capture_is_opt_in_and_truncation_is_flagged(self) -> None:
        from pyloggle.client import ClientConfig
        from pyloggle.fastapi import PyloggleFastAPIMiddleware

        async def app_reads_body_and_ok(scope, receive, send):
            # Ensure middleware receive wrapper doesn't break body consumption.
            while True:
                msg = await receive()
                if not msg or msg.get("type") != "http.request":
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
            cfg = ClientConfig(
                emit_request_latency=True,
                emit_request_body=emit_request_body,
                max_request_body_bytes=max_request_body_bytes,
                user_id_salt=None,
            )
            c = _CaptureClient(cfg)
            mw = PyloggleFastAPIMiddleware(app_reads_body_and_ok, c)  # type: ignore[arg-type]
            msgs = [{"type": "http.request", "body": raw, "more_body": False}]

            async def _receive():
                if msgs:
                    return msgs.pop(0)
                return {"type": "http.request", "body": b"", "more_body": False}

            async def _send(_message):
                return

            await mw(scope, _receive, _send)
            return c.emitted[0]

        ev_off = asyncio.run(_run_once(emit_request_body=False, max_request_body_bytes=4096))
        self.assertNotIn("body", (ev_off.get("http") or {}))
        self.assertNotIn("request_body_truncated", (ev_off.get("attrs") or {}))
        self.assertEqual((ev_off.get("http") or {}).get("remote_addr"), "127.0.0.1")
        self.assertEqual((ev_off.get("http") or {}).get("server_addr"), "example.com:443")

        ev_on = asyncio.run(_run_once(emit_request_body=True, max_request_body_bytes=4096))
        self.assertEqual((ev_on.get("http") or {}).get("body"), {"hello": "world"})
        self.assertNotIn("request_body_truncated", (ev_on.get("attrs") or {}))
        self.assertEqual((ev_on.get("http") or {}).get("remote_addr"), "127.0.0.1")
        self.assertEqual((ev_on.get("http") or {}).get("server_addr"), "example.com:443")

        ev_trunc = asyncio.run(_run_once(emit_request_body=True, max_request_body_bytes=8))
        # Truncated JSON should fall back to text (because it's incomplete).
        self.assertEqual((ev_trunc.get("http") or {}).get("body"), raw[:8].decode("utf-8"))
        self.assertTrue((ev_trunc.get("attrs") or {}).get("request_body_truncated"))
        self.assertEqual((ev_trunc.get("http") or {}).get("remote_addr"), "127.0.0.1")
        self.assertEqual((ev_trunc.get("http") or {}).get("server_addr"), "example.com:443")

    def test_flask_includes_url_and_body_is_opt_in_with_truncation(self) -> None:
        try:
            from flask import Flask
        except Exception:
            self.skipTest("Flask not installed (optional dependency)")

        from pyloggle.client import ClientConfig
        from pyloggle.flask import init_app

        def _run_once(*, emit_request_body: bool, max_request_body_bytes: int):
            cfg = ClientConfig(
                emit_request_latency=True,
                emit_request_body=emit_request_body,
                max_request_body_bytes=max_request_body_bytes,
                user_id_salt=None,
            )
            c = _CaptureClient(cfg)
            app = Flask(__name__)
            app.testing = True
            init_app(app, c)  # type: ignore[arg-type]

            @app.post("/ok")
            def _ok():
                return ("", 204)

            payload = b"0123456789"
            with app.test_client() as tc:
                resp = tc.post("/ok?x=1", data=payload, headers={"Content-Type": "text/plain"})
                self.assertEqual(resp.status_code, 204)
            self.assertEqual(len(c.emitted), 1)
            return c.emitted[0], payload

        ev_off, payload = _run_once(emit_request_body=False, max_request_body_bytes=4096)
        self.assertIn("/ok", str((ev_off.get("http") or {}).get("url")))
        self.assertIn("?x=1", str((ev_off.get("http") or {}).get("url")))
        self.assertNotIn("body", (ev_off.get("http") or {}))
        self.assertTrue(bool((ev_off.get("http") or {}).get("remote_addr")))
        self.assertTrue(bool((ev_off.get("http") or {}).get("server_addr")))

        ev_on, payload = _run_once(emit_request_body=True, max_request_body_bytes=4096)
        self.assertEqual((ev_on.get("http") or {}).get("body"), payload.decode("utf-8"))
        self.assertNotIn("request_body_truncated", (ev_on.get("attrs") or {}))
        self.assertTrue(bool((ev_on.get("http") or {}).get("remote_addr")))
        self.assertTrue(bool((ev_on.get("http") or {}).get("server_addr")))

        ev_trunc, payload = _run_once(emit_request_body=True, max_request_body_bytes=8)
        self.assertEqual((ev_trunc.get("http") or {}).get("body"), payload[:8].decode("utf-8"))
        self.assertTrue((ev_trunc.get("attrs") or {}).get("request_body_truncated"))
        self.assertTrue(bool((ev_trunc.get("http") or {}).get("remote_addr")))
        self.assertTrue(bool((ev_trunc.get("http") or {}).get("server_addr")))

    def test_flask_error_event_includes_url_and_body_opt_in_with_truncation(self) -> None:
        try:
            from flask import Flask
        except Exception:
            self.skipTest("Flask not installed (optional dependency)")

        from pyloggle.client import ClientConfig
        from pyloggle.flask import init_app

        def _run_once(*, emit_request_body: bool, max_request_body_bytes: int):
            cfg = ClientConfig(
                emit_request_latency=False,  # error event should still include URL/body
                emit_request_body=emit_request_body,
                max_request_body_bytes=max_request_body_bytes,
                user_id_salt=None,
            )
            c = _CaptureClient(cfg)
            app = Flask(__name__)
            app.testing = True
            app.config["PROPAGATE_EXCEPTIONS"] = True
            init_app(app, c)  # type: ignore[arg-type]

            @app.post("/boom")
            def _boom():
                raise RuntimeError("boom")

            payload = b"0123456789"
            with app.test_client() as tc:
                try:
                    tc.post("/boom?y=2", data=payload, headers={"Content-Type": "text/plain"})
                except RuntimeError:
                    pass
            self.assertEqual(len(c.emitted), 1)
            return c.emitted[0], payload

        ev_off, payload = _run_once(emit_request_body=False, max_request_body_bytes=4096)
        self.assertIn("/boom", str((ev_off.get("http") or {}).get("url")))
        self.assertIn("?y=2", str((ev_off.get("http") or {}).get("url")))
        self.assertNotIn("body", (ev_off.get("http") or {}))
        self.assertTrue(bool((ev_off.get("http") or {}).get("remote_addr")))
        self.assertTrue(bool((ev_off.get("http") or {}).get("server_addr")))

        ev_trunc, payload = _run_once(emit_request_body=True, max_request_body_bytes=8)
        self.assertEqual((ev_trunc.get("http") or {}).get("body"), payload[:8].decode("utf-8"))
        self.assertTrue((ev_trunc.get("attrs") or {}).get("request_body_truncated"))
        self.assertTrue(bool((ev_trunc.get("http") or {}).get("remote_addr")))
        self.assertTrue(bool((ev_trunc.get("http") or {}).get("server_addr")))


if __name__ == "__main__":
    unittest.main()


