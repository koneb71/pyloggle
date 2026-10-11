import logging

from pyloggle import scope
from pyloggle.logging_handler import PyloggleHandler
from pyloggle.stack import mark_captured


class _Capture:
    def __init__(self):
        class _Cfg:
            user_id_salt = None
            max_breadcrumbs = 50

        self.config = _Cfg()
        self.events = []

    def emit(self, ev):
        self.events.append(ev)


def _logger(name="t.handler"):
    lg = logging.getLogger(name)
    lg.handlers = []
    lg.propagate = False
    lg.setLevel(logging.DEBUG)
    return lg


def test_timestamp_levels_extra_and_code_attrs():
    c = _Capture()
    lg = _logger()
    lg.addHandler(PyloggleHandler(c))
    lg.warning("careful %s", 1, extra={"order_id": 7})
    lg.critical("down")
    ev = c.events[0]
    assert ev["ts"].endswith("Z") and "T" in ev["ts"] and "." in ev["ts"]  # RFC3339 with ms
    assert ev["level"] == "warn" and c.events[1]["level"] == "fatal"
    assert ev["message"] == "careful 1" and ev["logger"] == "t.handler"
    assert ev["attrs"]["order_id"] == 7
    assert ev["attrs"]["code.function"] == "test_timestamp_levels_extra_and_code_attrs" and ev["attrs"]["code.lineno"] > 0
    assert ev["request_id"]  # ensured for scripts


def test_exc_info_becomes_exception_and_marks_it():
    c = _Capture()
    lg = _logger()
    lg.addHandler(PyloggleHandler(c))
    try:
        1 / 0
    except ZeroDivisionError as exc:
        lg.exception("boom")
        boom = exc
    ev = c.events[0]
    assert ev["level"] == "error" and ev["exception"]["type"] == "ZeroDivisionError"
    assert ev["exception"]["frames"][-1]["context"] == "1 / 0"
    assert ev["mechanism"] == {"type": "logging", "handled": True}
    assert len(ev["event_id"]) == 32
    # the same exception logged again (e.g. by a framework) is a duplicate: no second issue
    lg.error("again", exc_info=(type(boom), boom, boom.__traceback__))
    dup = c.events[1]
    assert dup["skip_issue"] is True and dup["attrs"]["pyloggle.duplicate_of"] == ev["event_id"]
    assert "exception" not in dup


def test_already_captured_exception_is_flagged():
    c = _Capture()
    lg = _logger()
    lg.addHandler(PyloggleHandler(c))
    e = RuntimeError("x")
    mark_captured(e, "evt1")
    lg.error("framework log", exc_info=(RuntimeError, e, None))
    assert c.events[0]["skip_issue"] is True and c.events[0]["attrs"]["pyloggle.duplicate_of"] == "evt1"


def test_breadcrumbs_from_records_below_error():
    c = _Capture()
    lg = _logger()
    lg.addHandler(PyloggleHandler(c))
    lg.info("step 1")
    lg.debug("step 2")
    lg.error("fail")
    crumbs = list(scope.iter_breadcrumbs(10))
    assert [b["message"] for b in crumbs] == ["step 1", "step 2"]
    assert crumbs[0]["category"] == "t.handler" and crumbs[0]["type"] == "log" and crumbs[0]["level"] == "info"
