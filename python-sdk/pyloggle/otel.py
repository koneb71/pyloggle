"""Optional OpenTelemetry bridge. When an OTEL span is active its ids are used for correlation
(the event lands next to the span in the trace view), and a captured exception stamps the span
with ``pyloggle.event_id`` so the server does not turn the span's own failure into a second
issue. Everything here is a no-op without the ``opentelemetry-api`` package."""
from __future__ import annotations

from typing import Optional, Tuple

try:  # pragma: no cover - depends on optional dependency
    from opentelemetry import trace as _trace  # type: ignore
except Exception:  # pragma: no cover
    _trace = None

EVENT_ID_ATTR = "pyloggle.event_id"


def _current_span():
    if _trace is None:
        return None
    try:
        span = _trace.get_current_span()
        ctx = span.get_span_context()
        if not ctx or not ctx.is_valid:
            return None
        return span
    except Exception:
        return None


def current_ids() -> Tuple[Optional[str], Optional[str]]:
    """(trace_id, span_id) of the active span as 32/16 hex chars, or (None, None)."""
    span = _current_span()
    if span is None:
        return None, None
    try:
        ctx = span.get_span_context()
        return format(ctx.trace_id, "032x"), format(ctx.span_id, "016x")
    except Exception:
        return None, None


def current_trace_id() -> Optional[str]:
    return current_ids()[0]


def current_span_id() -> Optional[str]:
    return current_ids()[1]


def server_span_active() -> bool:
    """True when a recording OpenTelemetry server span covers this request: the span already
    produces the request line, so the integrations skip theirs."""
    span = _current_span()
    if span is None:
        return False
    try:
        if not span.is_recording():
            return False
    except Exception:
        return False
    kind = getattr(span, "kind", None)
    if kind is None:
        return True
    try:
        from opentelemetry.trace import SpanKind  # type: ignore

        return kind == SpanKind.SERVER
    except Exception:
        return True


def annotate_captured(event_id: str) -> bool:
    """Stamp the active span with the event id of the exception just captured."""
    span = _current_span()
    if span is None or not event_id:
        return False
    try:
        if not span.is_recording():
            return False
        span.set_attribute(EVENT_ID_ATTR, str(event_id))
        return True
    except Exception:
        return False
