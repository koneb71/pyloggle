import sys

import pytest

from pyloggle.stack import captured_event_id, exception_to_payload, is_in_app, mark_captured, safe_repr


def _inner(password="hunter2"):
    secret_token = "abc"
    amount = -1
    raise ValueError(f"bad amount {amount}")


def _outer():
    try:
        _inner()
    except ValueError as e:
        raise RuntimeError("wrapped") from e


def test_frames_order_context_locals_and_chain():
    try:
        _outer()
    except RuntimeError as exc:
        p = exception_to_payload(exc)
    assert p["type"] == "RuntimeError" and p["message"] == "wrapped" and p["module"] == ""
    frames = p["frames"]
    assert frames[-1]["function"] == "_outer"  # crash site last
    assert frames[-1]["context"].startswith("raise RuntimeError")
    assert frames[-1]["filename"].endswith("test_stack.py") and frames[-1]["abs_path"].startswith("/")
    assert frames[-1]["module"] == "tests.test_stack" or frames[-1]["module"].endswith("test_stack")
    assert frames[-1]["lineno"] > 0 and frames[-1]["in_app"] is True
    assert len(frames[-1]["pre_context"]) == 5 and isinstance(frames[-1]["post_context"], list)
    # chain: the ValueError is the direct cause, with its own frames and redacted locals
    assert len(p["chain"]) == 1
    cause = p["chain"][0]
    assert cause["type"] == "ValueError" and cause["is_cause"] is True and cause["relation"] == "cause"
    inner = cause["frames"][-1]
    assert inner["function"] == "_inner"
    assert inner["vars"]["amount"] == "-1"
    assert inner["vars"]["password"] == "[REDACTED]" and inner["vars"]["secret_token"] == "[REDACTED]"
    if sys.version_info >= (3, 11):
        assert inner["colno"] >= 1


def test_context_chain_and_suppress():
    try:
        try:
            {}["k"]
        except KeyError:
            raise ValueError("during")
    except ValueError as exc:
        p = exception_to_payload(exc)
    assert p["chain"][0]["type"] == "KeyError" and p["chain"][0]["relation"] == "context" and p["chain"][0]["is_cause"] is False
    try:
        try:
            {}["k"]
        except KeyError:
            raise ValueError("suppressed") from None
    except ValueError as exc:
        p = exception_to_payload(exc)
    assert "chain" not in p


def test_notes_and_groups():
    try:
        raise ValueError("x")
    except ValueError as exc:
        if hasattr(exc, "add_note"):
            exc.add_note("retrying later")
        p = exception_to_payload(exc)
    if sys.version_info >= (3, 11):
        assert p["notes"] == ["retrying later"]
        try:
            raise ExceptionGroup("many", [ValueError("a"), TypeError("b")])  # noqa: F821
        except ExceptionGroup as eg:  # noqa: F821
            g = exception_to_payload(eg)
        assert [m["type"] for m in g["group"]] == ["ValueError", "TypeError"]
        assert g["group"][0]["relation"] == "group"


class _Cfg:
    with_locals = False
    context_lines = 1
    max_frames = 2
    in_app_include = ()
    in_app_exclude = ("tests",)


def test_config_controls_locals_context_frames_and_in_app():
    def a():
        b()

    def b():
        c()

    def c():
        raise OSError("deep")

    try:
        a()
    except OSError as exc:
        p = exception_to_payload(exc, config=_Cfg())
    frames = p["frames"]
    assert len(frames) == 2 and frames[-1]["function"].endswith("c")  # head + tail kept
    assert "vars" not in frames[-1]
    assert len(frames[-1].get("pre_context", [])) == 1
    assert frames[-1]["in_app"] is False  # excluded by module prefix


def test_in_app_rules():
    assert is_in_app("/srv/app/x.py", "app.x") is True
    assert is_in_app("/x/site-packages/lib/y.py", "lib.y") is False
    assert is_in_app("/x/site-packages/lib/y.py", "lib.y", include=("lib",)) is True
    assert is_in_app("/srv/app/x.py", "app.x", exclude=("/srv/app",)) is False
    assert is_in_app("<frozen importlib._bootstrap>", "importlib._bootstrap") is False


def test_safe_repr_never_raises():
    class Bad:
        def __repr__(self):
            raise RuntimeError("no repr")

    out = safe_repr(Bad())  # must not raise; reprlib falls back to a placeholder
    assert "Bad" in out
    assert len(safe_repr("x" * 10_000, max_len=50)) <= 50
    assert safe_repr([1, 2, 3]) == "[1, 2, 3]"


def test_mark_captured_roundtrip():
    e = ValueError("x")
    assert captured_event_id(e) is None
    mark_captured(e, "abc")
    assert captured_event_id(e) == "abc"
    with pytest.raises(ValueError):
        raise e
