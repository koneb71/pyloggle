from __future__ import annotations

import contextvars
import uuid
from typing import Optional


request_id_var: contextvars.ContextVar[Optional[str]] = contextvars.ContextVar("pyloggle_request_id", default=None)
trace_id_var: contextvars.ContextVar[Optional[str]] = contextvars.ContextVar("pyloggle_trace_id", default=None)
user_id_hash_var: contextvars.ContextVar[Optional[str]] = contextvars.ContextVar("pyloggle_user_id_hash", default=None)


def get_request_id() -> Optional[str]:
    return request_id_var.get()


def set_request_id(v: Optional[str]) -> None:
    request_id_var.set(v)


def ensure_request_id() -> str:
    rid = request_id_var.get()
    if rid:
        return rid
    rid = uuid.uuid4().hex
    request_id_var.set(rid)
    return rid


def set_trace_id(v: Optional[str]) -> None:
    trace_id_var.set(v)


def get_trace_id() -> Optional[str]:
    return trace_id_var.get()


def get_user_id_hash() -> Optional[str]:
    return user_id_hash_var.get()


def set_user_id_hash(v: Optional[str]) -> None:
    user_id_hash_var.set(v)


