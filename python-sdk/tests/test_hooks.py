import json
import os
import subprocess
import sys
import textwrap

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def _run(fake_ingest, code: str):
    env = dict(os.environ, PYLOGGLE_INGEST_URL=fake_ingest.url, PYLOGGLE_API_KEY="k", PYTHONPATH=ROOT)
    proc = subprocess.run([sys.executable, "-c", textwrap.dedent(code)], env=env, capture_output=True, text=True, timeout=30)
    return proc


def test_uncaught_exception_in_script_is_reported_and_flushed(fake_ingest):
    proc = _run(fake_ingest, """
        import pyloggle
        pyloggle.init(service="script", env="test", flush_interval_s=0.05)
        pyloggle.add_breadcrumb("about to fail")
        def main():
            raise RuntimeError("uncaught in script")
        main()
    """)
    assert proc.returncode == 1 and "RuntimeError: uncaught in script" in proc.stderr  # default hook still prints
    evs = fake_ingest.wait_for(1)
    ev = [e for e in evs if e.get("exception")][0]
    assert ev["level"] == "fatal" and ev["mechanism"] == {"type": "excepthook", "handled": False}
    assert ev["exception"]["frames"][-1]["function"] == "main"
    assert ev["breadcrumbs"][0]["message"] == "about to fail"


def test_uncaught_exception_in_thread(fake_ingest):
    proc = _run(fake_ingest, """
        import threading, pyloggle
        pyloggle.init(service="script", env="test", flush_interval_s=0.05)
        def work():
            raise ValueError("in thread")
        t = threading.Thread(target=work); t.start(); t.join()
        pyloggle.flush(5)
    """)
    assert proc.returncode == 0
    ev = [e for e in fake_ingest.wait_for(1) if e.get("exception")][0]
    assert ev["exception"]["type"] == "ValueError" and ev["mechanism"]["type"] == "threading"


def test_unhandled_asyncio_task_exception(fake_ingest):
    proc = _run(fake_ingest, """
        import asyncio, pyloggle
        pyloggle.init(service="script", env="test", flush_interval_s=0.05)
        async def bad():
            raise LookupError("in task")
        async def main():
            pyloggle.install_asyncio_hook()
            t = asyncio.ensure_future(bad())
            await asyncio.sleep(0.05)
            del t
            import gc; gc.collect()
            await asyncio.sleep(0.05)
        asyncio.run(main())
        pyloggle.flush(5)
    """)
    assert proc.returncode == 0, proc.stderr
    ev = [e for e in fake_ingest.wait_for(1) if e.get("exception")][0]
    assert ev["exception"]["type"] == "LookupError" and ev["mechanism"]["type"] == "asyncio"


def test_plain_client_installs_hooks_by_default(fake_ingest):
    proc = _run(fake_ingest, """
        from pyloggle import Client, ClientConfig
        import os
        c = Client(ClientConfig(ingest_url=os.environ["PYLOGGLE_INGEST_URL"], api_key="k", service="legacy", flush_interval_s=0.05))
        raise RuntimeError("legacy client, zero code changes")
    """)
    assert proc.returncode == 1
    ev = [e for e in fake_ingest.wait_for(1) if e.get("exception")][0]
    assert ev["service"] == "legacy" and ev["mechanism"]["type"] == "excepthook"
