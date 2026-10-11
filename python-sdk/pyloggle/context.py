"""Request-scoped identifiers. Backed by the scope since 0.2 (same functions as 0.1)."""
from __future__ import annotations

import uuid
from typing import Optional

from . import scope as _scope


def get_request_id() -> Optional[str]:
    return _scope.effective_scope().request_id


def set_request_id(v: Optional[str]) -> None:
    _scope.effective_scope().request_id = v


def ensure_request_id() -> str:
    sc = _scope.effective_scope()
    if sc.request_id:
        return sc.request_id
    sc.request_id = uuid.uuid4().hex
    return sc.request_id


def set_trace_id(v: Optional[str]) -> None:
    _scope.effective_scope().trace_id = v


def get_trace_id() -> Optional[str]:
    return _scope.effective_scope().trace_id


def set_span_id(v: Optional[str]) -> None:
    _scope.effective_scope().span_id = v


def get_span_id() -> Optional[str]:
    return _scope.effective_scope().span_id


def get_user_id_hash() -> Optional[str]:
    u = _scope.effective_scope().user
    if not u:
        return None
    return u.get("id_hash") or None


def set_user_id_hash(v: Optional[str]) -> None:
    sc = _scope.effective_scope()
    if not v:
        if sc.user:
            sc.user.pop("id_hash", None)
            if not sc.user:
                sc.user = None
        return
    if sc.user is None:
        sc.user = {}
    sc.user["id_hash"] = v


def parse_traceparent(value: Optional[str]) -> tuple[Optional[str], Optional[str]]:
    """W3C traceparent: 00-<32 hex trace id>-<16 hex span id>-<flags>."""
    if not value:
        return None, None
    parts = value.strip().split("-")
    if len(parts) < 3:
        return None, None
    trace_id, span_id = parts[1].lower(), parts[2].lower()
    if len(trace_id) != 32 or len(span_id) != 16 or set(trace_id + span_id) - set("0123456789abcdef"):
        return None, None
    if trace_id == "0" * 32:
        return None, None
    return trace_id, span_id
