"""Optional OpenTelemetry bridge: if an OTEL span is active, its ids are used for correlation."""
from __future__ import annotations

from typing import Optional

try:  # pragma: no cover - depends on optional dependency
    from opentelemetry import trace as _trace  # type: ignore
except Exception:  # pragma: no cover
    _trace = None


def current_trace_id() -> Optional[str]:
    if _trace is None:
        return None
    try:
        span = _trace.get_current_span()
        ctx = span.get_span_context()
        if not ctx or not ctx.is_valid:
            return None
        return format(ctx.trace_id, "032x")
    except Exception:
        return None


def current_span_id() -> Optional[str]:
    if _trace is None:
        return None
    try:
        span = _trace.get_current_span()
        ctx = span.get_span_context()
        if not ctx or not ctx.is_valid:
            return None
        return format(ctx.span_id, "016x")
    except Exception:
        return None
