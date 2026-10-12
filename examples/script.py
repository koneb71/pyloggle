"""Plain script: one call to init(), then every log line, breadcrumb and uncaught exception
reaches pyloggle. Set PYLOGGLE_INGEST_URL / PYLOGGLE_API_KEY (and optionally
PYLOGGLE_PROJECT_ID, PYLOGGLE_ENV, PYLOGGLE_RELEASE) in the environment."""
import logging

import pyloggle

logging.basicConfig(level=logging.INFO)  # before init(): basicConfig does nothing once a handler exists
pyloggle.init(service="example-script", env="dev")
log = logging.getLogger("example")

pyloggle.set_tag("job", "nightly-report")
pyloggle.set_user({"id": "user-123"})  # hashed; raw ids/emails only with send_default_pii=True


def load_orders(path):
    log.info("loading orders from %s", path)
    pyloggle.add_breadcrumb("opening file", category="io", data={"path": path})
    raise FileNotFoundError(path)


def main():
    log.info("starting")
    try:
        1 / 0
    except ZeroDivisionError:
        log.exception("division failed (reported via logging)")
    try:
        load_orders("/tmp/orders.csv")
    except FileNotFoundError as exc:
        pyloggle.capture_exception(exc, tags={"stage": "load"})
    # An uncaught exception is reported by the installed excepthook and flushed before exit.
    raise RuntimeError("boom from script (uncaught)")


if __name__ == "__main__":
    main()
