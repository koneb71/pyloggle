"""Request/task scope: the context an event is enriched with.

A Scope lives in a contextvar, so every request (thread or asyncio task) gets its own; the
framework integrations push one at request start and reset it at the end. Outside a request
(scripts, startup code) the process-wide global scope is used, so tags and user set at startup
apply to every event.
"""
from __future__ import annotations

import contextvars
import threading
import time
from collections import deque
from dataclasses import dataclass, field
from typing import Any, Callable, Deque, Dict, Iterator, Optional, Tuple

MAX_BREADCRUMBS_DEFAULT = 100


@dataclass
class Scope:
    request_id: Optional[str] = None
    trace_id: Optional[str] = None
    span_id: Optional[str] = None
    transaction: Optional[str] = None
    user: Optional[Dict[str, Any]] = None  # id_hash plus optional raw id/email/username/ip
    tags: Dict[str, str] = field(default_factory=dict)
    extra: Dict[str, Any] = field(default_factory=dict)  # context name -> dict
    http: Optional[Dict[str, Any]] = None
    # The request's headers and body, read on demand for an error raised inside the request:
    # returns (http fields, body truncated).
    request_details: Optional[Callable[[], Tuple[Dict[str, Any], bool]]] = None
    breadcrumbs: Deque[Dict[str, Any]] = field(default_factory=lambda: deque(maxlen=MAX_BREADCRUMBS_DEFAULT))

    def copy(self) -> "Scope":
        s = Scope(
            request_id=self.request_id,
            trace_id=self.trace_id,
            span_id=self.span_id,
            transaction=self.transaction,
            user=dict(self.user) if self.user else None,
            tags=dict(self.tags),
            extra={k: dict(v) if isinstance(v, dict) else v for k, v in self.extra.items()},
            http=dict(self.http) if self.http else None,
            request_details=self.request_details,
        )
        s.breadcrumbs = deque(self.breadcrumbs, maxlen=self.breadcrumbs.maxlen)
        return s


_scope_var: contextvars.ContextVar[Optional[Scope]] = contextvars.ContextVar("pyloggle_scope", default=None)
_global_scope = Scope()
_global_lock = threading.Lock()


def global_scope() -> Scope:
    return _global_scope


def current_scope() -> Optional[Scope]:
    """The request/task scope if one was pushed, else None."""
    return _scope_var.get()


def effective_scope() -> Scope:
    """Where set_tag/set_user/add_breadcrumb write: the request scope, else the global one."""
    sc = _scope_var.get()
    return sc if sc is not None else _global_scope


def push_scope(scope: Optional[Scope] = None, *, inherit: bool = True) -> contextvars.Token:
    """Activate a scope in the current context; returns the token for reset_scope()."""
    if scope is None:
        base = _scope_var.get() if inherit else None
        scope = base.copy() if base is not None else Scope()
        if inherit and base is None:
            # Start a request scope from the global one so startup tags/user/breadcrumbs carry over.
            scope.tags = dict(_global_scope.tags)
            scope.user = dict(_global_scope.user) if _global_scope.user else None
            scope.extra = {k: (dict(v) if isinstance(v, dict) else v) for k, v in _global_scope.extra.items()}
            with _global_lock:
                scope.breadcrumbs = deque(_global_scope.breadcrumbs, maxlen=_global_scope.breadcrumbs.maxlen)
    return _scope_var.set(scope)


def reset_scope(token: Optional[contextvars.Token]) -> None:
    if token is None:
        return
    try:
        _scope_var.reset(token)
    except RuntimeError:
        # Already reset (frameworks may run teardown twice): nothing to do.
        return
    except (ValueError, LookupError):
        # Token from another context (e.g. teardown ran elsewhere): clear instead.
        _scope_var.set(None)


def clear_scope() -> None:
    _scope_var.set(None)


def add_breadcrumb(
    message: str,
    *,
    category: str = "default",
    level: str = "info",
    type: str = "default",
    data: Optional[Dict[str, Any]] = None,
    ts: Optional[float] = None,
) -> None:
    crumb: Dict[str, Any] = {
        "ts": _iso(ts if ts is not None else time.time()),
        "type": type,
        "category": category,
        "level": level,
        "message": str(message)[:1000],
    }
    if data:
        crumb["data"] = {str(k): v for k, v in data.items()}
    sc = effective_scope()
    if sc is _global_scope:
        with _global_lock:
            sc.breadcrumbs.append(crumb)
    else:
        sc.breadcrumbs.append(crumb)


def iter_breadcrumbs(limit: int) -> Iterator[Dict[str, Any]]:
    sc = effective_scope()
    crumbs = list(sc.breadcrumbs)
    if limit > 0:
        crumbs = crumbs[-limit:]
    return iter(crumbs)


def set_tag(key: str, value: Any) -> None:
    effective_scope().tags[str(key)] = _stringify(value)


def set_tags(tags: Dict[str, Any]) -> None:
    for k, v in (tags or {}).items():
        set_tag(k, v)


def remove_tag(key: str) -> None:
    effective_scope().tags.pop(str(key), None)


def set_context(name: str, data: Optional[Dict[str, Any]]) -> None:
    sc = effective_scope()
    if data is None:
        sc.extra.pop(str(name), None)
    else:
        sc.extra[str(name)] = dict(data)


def set_user(user: Optional[Dict[str, Any]]) -> None:
    effective_scope().user = dict(user) if user else None


def _stringify(v: Any) -> str:
    if isinstance(v, bool):
        return "true" if v else "false"
    if v is None:
        return ""
    return str(v)


def _iso(ts: float) -> str:
    ms = int(round((ts - int(ts)) * 1000))
    if ms >= 1000:
        ts += 1
        ms = 0
    return time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(ts)) + ".%03dZ" % ms


def iso_now() -> str:
    return _iso(time.time())
