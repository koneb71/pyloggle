"""Celery tasks instrumented with pyloggle. Each task run gets its own scope (task name, id,
queue, retries) and failures become issues that carry the publisher's request id.

    pip install "pyloggle[celery]"
    PYLOGGLE_INGEST_URL=http://127.0.0.1:8080 PYLOGGLE_API_KEY=pl_... celery -A examples.celery_app worker -l info
    python -c "from examples.celery_app import boom, add; add.delay(1, 2); boom.delay('fly')"

With no broker configured the script runs an embedded worker on an in-memory broker and
enqueues both tasks itself:   python examples/celery_app.py
"""
import logging
import os

from celery import Celery

import pyloggle
import pyloggle.celery

logging.basicConfig(level=logging.INFO)

app = Celery("example", broker=os.getenv("CELERY_BROKER_URL", "memory://"), backend=os.getenv("CELERY_RESULT_BACKEND", "cache+memory://"))

pyloggle.init(service="example-celery", env="dev")
pyloggle.celery.init(emit_task_events=True)


@app.task(name="example.add")
def add(a, b):
    logging.getLogger(__name__).info("adding %s + %s", a, b)
    return a + b


@app.task(name="example.boom", bind=True, max_retries=1)
def boom(self, what):
    pyloggle.add_breadcrumb(f"trying to {what}", category="demo")
    if self.request.retries == 0:
        raise self.retry(countdown=0, exc=RuntimeError("not yet"))
    raise ValueError(f"cannot {what}")


if __name__ == "__main__":
    from celery.contrib.testing.worker import start_worker

    with pyloggle.push_scope():
        pyloggle.set_tag("demo", "celery")
        with start_worker(app, perform_ping_check=False, loglevel="INFO"):
            print("add ->", add.delay(1, 2).get(timeout=10))
            boom.delay("fly").get(timeout=10, propagate=False)
    pyloggle.flush()
