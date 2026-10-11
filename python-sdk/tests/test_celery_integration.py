import os
import time

import pytest

celery = pytest.importorskip("celery")

from celery import Celery  # noqa: E402

import pyloggle.celery as pcl  # noqa: E402
from pyloggle import scope  # noqa: E402


class _Capture:
    def __init__(self, **cfg):
        from pyloggle import ClientConfig

        self.config = ClientConfig(**cfg)
        self.emitted = []

    def emit(self, ev):
        self.emitted.append(ev)

    def _after_fork(self):
        pass


eager = Celery("eager", broker="memory://", backend="cache+memory://")
eager.conf.update(task_always_eager=True, task_eager_propagates=False, task_store_eager_result=True)


@eager.task(name="tests.add")
def add(a, b):
    return a + b


@eager.task(name="tests.fail")
def fail(what):
    raise ValueError(f"cannot {what}")


@eager.task(name="tests.peek", bind=True)
def peek(self):
    sc = scope.effective_scope()
    return {"rid": sc.request_id, "trace": sc.trace_id, "transaction": sc.transaction, "tags": dict(sc.tags), "crumbs": [b["category"] for b in sc.breadcrumbs]}


@eager.task(name="tests.flaky", bind=True, max_retries=1)
def flaky(self):
    if self.request.retries == 0:
        raise self.retry(countdown=0, exc=RuntimeError("try again"))
    return "second time lucky"


@pytest.fixture
def capture():
    c = _Capture(user_id_salt=None)
    prev = pcl._client
    pcl.init(c)
    yield c
    pcl._client = prev
    pcl._emit_task_events = False


def test_failure_is_one_event_with_task_context(capture):
    r = fail.apply(args=("fly",), task_id="tid-1")
    assert r.failed()
    assert len(capture.emitted) == 1
    ev = capture.emitted[0]
    assert ev["exception"]["type"] == "ValueError" and ev["exception"]["message"] == "cannot fly"
    assert ev["mechanism"] == {"type": "celery", "handled": False} and ev["transaction"] == "tests.fail"
    assert ev["extra"]["celery"]["task"] == "tests.fail" and ev["extra"]["celery"]["task_id"] == "tid-1"
    assert ev["extra"]["celery"]["args"] == "('fly',)" and ev["attrs"]["celery.task"] == "tests.fail"
    assert isinstance(ev["duration_ms"], float)
    assert scope.current_scope() is None


def test_scope_per_task_with_request_id_from_headers(capture):
    out = peek.apply(task_id="tid-2", headers={"pyloggle-request-id": "rid-web", "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}).get()
    assert out["rid"] == "rid-web" and out["trace"] == "4bf92f3577b34da6a3ce929d0e0e4736"
    assert out["transaction"] == "tests.peek" and out["tags"]["celery.task"] == "tests.peek" and out["tags"]["celery.task_id"] == "tid-2"
    assert out["tags"]["celery.retries"] == "0" and out["crumbs"] == ["celery.task"]
    # Without headers the task id is the request id.
    out = peek.apply(task_id="tid-3").get()
    assert out["rid"] == "tid-3" and out["trace"] is None
    assert capture.emitted == [] and scope.current_scope() is None


def test_success_emits_task_event_only_when_asked(capture):
    assert add.apply(args=(1, 2)).get() == 3
    assert capture.emitted == []
    pcl.init(capture, emit_task_events=True)
    assert add.apply(args=(2, 2), task_id="tid-4").get() == 4
    assert len(capture.emitted) == 1
    ev = capture.emitted[0]
    assert ev["message"] == "task" and ev["level"] == "info" and ev["transaction"] == "tests.add"
    assert ev["attrs"]["celery.state"] == "SUCCESS" and isinstance(ev["duration_ms"], float)
    # A failing run has the error event and no task event.
    fail.apply(args=("x",))
    kinds = [e.get("message") for e in capture.emitted[1:]]
    assert kinds == ["cannot x"]


def test_retry_is_a_warning_not_an_issue(capture):
    r = flaky.apply()
    assert r.get() == "second time lucky"
    warns = [e for e in capture.emitted if e["level"] == "warn"]
    assert len(warns) == 1 and "retrying" in warns[0]["message"] and warns[0]["attrs"]["celery.task"] == "tests.flaky"
    assert [e for e in capture.emitted if e.get("exception")] == []


def test_publish_stamps_headers_and_leaves_breadcrumb(capture):
    """A real broker round trip: the web request's id travels in the message headers and the
    worker's error event carries it; the publisher's scope gets an enqueue breadcrumb."""
    from celery.contrib.testing.worker import start_worker

    app = Celery("worker", broker="memory://", backend="cache+memory://")
    app.conf.update(task_always_eager=False, worker_hijack_root_logger=False)

    @app.task(name="tests.worker_fail")
    def worker_fail(n):
        raise KeyError(f"missing {n}")

    @app.task(name="tests.worker_peek")
    def worker_peek():
        sc = scope.effective_scope()
        return {"rid": sc.request_id, "trace": sc.trace_id, "queue": sc.tags.get("celery.queue")}

    with start_worker(app, perform_ping_check=False, loglevel="ERROR", shutdown_timeout=10):
        token = scope.push_scope()
        try:
            sc = scope.effective_scope()
            sc.request_id, sc.trace_id, sc.span_id = "rid-web", "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"
            res = worker_peek.delay()
            out = res.get(timeout=15)
            assert out == {"rid": "rid-web", "trace": "4bf92f3577b34da6a3ce929d0e0e4736", "queue": "celery"}
            res = worker_fail.delay(7)
            res.get(timeout=15, propagate=False)
            crumbs = [b for b in sc.breadcrumbs if b["category"] == "celery.publish"]
            assert len(crumbs) == 2 and crumbs[0]["message"] == "enqueued tests.worker_peek" and crumbs[0]["data"]["queue"] == "celery"
        finally:
            scope.reset_scope(token)
        deadline = time.time() + 5
        while time.time() < deadline and not [e for e in capture.emitted if e.get("exception")]:
            time.sleep(0.02)
    errs = [e for e in capture.emitted if e.get("exception")]
    assert len(errs) == 1
    assert errs[0]["exception"]["type"] == "KeyError" and errs[0]["extra"]["celery"]["queue"] == "celery"
    assert errs[0]["transaction"] == "tests.worker_fail"


def test_celery_error_log_line_is_marked_duplicate(make_client, fake_ingest):
    """Celery logs 'Task ... raised unexpected' with exc_info after the signal; the logging
    handler keeps it as a duplicate of the captured event."""
    import logging

    from pyloggle.logging_handler import PyloggleHandler, install_pyloggle_logging

    client = make_client()
    root = logging.getLogger()
    before = list(root.handlers)
    prev = pcl._client
    try:
        root.handlers = []
        install_pyloggle_logging(client)
        pcl.init(client)
        fail.apply(args=("log",))
        client.flush(2.0)
    finally:
        root.handlers = [h for h in before if not isinstance(h, PyloggleHandler)]
        pcl._client = prev
    events = fake_ingest.wait_for(2)
    exc_events = [e for e in events if e.get("exception")]
    dups = [e for e in events if e.get("skip_issue")]
    assert len(exc_events) == 1 and exc_events[0]["mechanism"]["type"] == "celery" and exc_events[0]["request_id"]
    assert len(dups) == 1 and dups[0]["logger"] == "celery.app.trace"
    assert dups[0]["attrs"]["pyloggle.duplicate_of"] == exc_events[0]["event_id"]
    assert exc_events[0]["tags"]["celery.task"] == "tests.fail" and exc_events[0]["breadcrumbs"][-1]["category"] == "celery.task"


def test_logging_handler_survives_worker_logging_setup(make_client):
    """Celery's worker replaces the root handlers (worker_hijack_root_logger); the integration
    re-installs the handler from the after_setup_logger signal."""
    import logging

    from celery import signals

    from pyloggle.logging_handler import PyloggleHandler, install_pyloggle_logging

    client = make_client()
    root = logging.getLogger()
    before = list(root.handlers)
    prev = pcl._client
    try:
        root.handlers = []
        install_pyloggle_logging(client)
        pcl.init(client)
        root.handlers = [logging.NullHandler()]  # what the hijack leaves behind
        signals.after_setup_logger.send(sender=None, logger=root, loglevel=logging.INFO, logfile=None, format="", colorize=False)
        assert any(isinstance(h, PyloggleHandler) for h in root.handlers)
        root.handlers = [logging.NullHandler()]
        pcl.init(client, capture_logs=False)
        signals.after_setup_logger.send(sender=None, logger=root, loglevel=logging.INFO, logfile=None, format="", colorize=False)
        assert not any(isinstance(h, PyloggleHandler) for h in root.handlers)
    finally:
        root.handlers = [h for h in before if not isinstance(h, PyloggleHandler)]
        pcl._client = prev
        pcl._capture_logs = True


@pytest.mark.skipif(not hasattr(os, "fork"), reason="fork only")
def test_client_restarts_its_sender_after_fork(make_client, fake_ingest):
    client = make_client()
    client.emit({"message": "from parent"})
    client.flush(2.0)
    pid = os.fork()
    if pid == 0:  # child
        try:
            client.emit({"message": "from child"})
            ok = client.flush(5.0)
        finally:
            os._exit(0 if ok else 3)
    _, status = os.waitpid(pid, 0)
    assert os.WEXITSTATUS(status) == 0
    events = fake_ingest.wait_for(2)
    msgs = {e["message"]: e for e in events}
    assert set(msgs) == {"from parent", "from child"}
    assert msgs["from child"]["runtime"]["pid"] == pid and msgs["from parent"]["runtime"]["pid"] == os.getpid()
