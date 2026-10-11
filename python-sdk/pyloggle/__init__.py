"""pyloggle — logging + error tracking SDK.

Quick start::

    import pyloggle
    pyloggle.init(service="checkout", env="prod")      # reads PYLOGGLE_* env vars
    ...
    pyloggle.capture_exception()                        # inside an except block
    pyloggle.add_breadcrumb("cache miss", category="cache")

Existing code built on ``Client(ClientConfig(...))`` keeps working unchanged.
"""
from __future__ import annotations

import asyncio
import functools
import inspect
import os
import sys
import uuid
from contextlib import contextmanager
from typing import Any, Callable, Dict, Iterable, Iterator, Mapping, Optional, Sequence

from . import scope as _scope
from ._version import __version__
from .client import Client, ClientConfig, normalize_level
from .context import get_user_id_hash, set_user_id_hash
from .scrub import ScrubConfig, default_scrub_config, scrub_event
from .stack import exception_to_payload, mark_captured
from .user import hash_user_id

__all__ = [
    "__version__",
    "Client",
    "ClientConfig",
    "ScrubConfig",
    "default_scrub_config",
    "scrub_event",
    "hash_user_id",
    "get_user_id_hash",
    "set_user_id_hash",
    "init",
    "get_client",
    "capture_exception",
    "capture_message",
    "add_breadcrumb",
    "set_user",
    "set_tag",
    "set_tags",
    "remove_tag",
    "set_context",
    "set_release",
    "push_scope",
    "capture_errors",
    "install_asyncio_hook",
    "flush",
    "close",
]

_client: Optional[Client] = None

_ENV_URL = ("PYLOGGLE_INGEST_URL", "PYLOGGLE_URL")
_ENV_KEY = ("PYLOGGLE_API_KEY", "PYLOGGLE_TOKEN")


def _env(*names: str) -> Optional[str]:
    for n in names:
        v = os.getenv(n)
        if v:
            return v
    return None


def init(
    ingest_url: Optional[str] = None,
    *,
    api_key: Optional[str] = None,
    service: Optional[str] = None,
    env: Optional[str] = None,
    project_id: Optional[str] = None,
    release: Optional[str] = None,
    integrations: Sequence[str] = ("logging", "excepthook", "http"),
    **config: Any,
) -> Client:
    """Create the global client from arguments and PYLOGGLE_* environment variables.

    integrations: "logging" installs the stdlib handler (root + framework loggers);
    "excepthook" installs the sys/threading/unraisable hooks; "http" records outgoing
    requests/httpx/urllib3 calls as breadcrumbs. Calling init() again replaces the previous
    client (after flushing it).
    """
    global _client
    cfg = ClientConfig(
        ingest_url=ingest_url or _env(*_ENV_URL) or ClientConfig.ingest_url,
        api_key=api_key or _env(*_ENV_KEY),
        service=service or _env("PYLOGGLE_SERVICE") or ClientConfig.service,
        env=env or _env("PYLOGGLE_ENV", "PYLOGGLE_ENVIRONMENT") or ClientConfig.env,
        project_id=project_id or _env("PYLOGGLE_PROJECT_ID") or ClientConfig.project_id,
        release=release or _env("PYLOGGLE_RELEASE"),
    )
    if _env("PYLOGGLE_DEBUG") in ("1", "true", "True", "yes"):
        cfg.debug = True
    cfg.install_excepthooks = "excepthook" in integrations
    for k, v in config.items():
        if not hasattr(cfg, k):
            raise TypeError(f"init() got an unexpected keyword argument {k!r}")
        setattr(cfg, k, v)
    prev = _client
    client = Client(cfg)
    if "logging" in integrations:
        from .logging_handler import install_pyloggle_logging

        install_pyloggle_logging(client)
        _repoint_logging(client)
    if "http" in integrations and getattr(cfg, "http_breadcrumbs", True):
        try:
            from .integrations import http as _http

            _http.install()
        except Exception:
            pass
    _client = client
    if prev is not None:
        try:
            prev.flush(1.0)
            prev.close()
        except Exception:
            pass
    return client


def _repoint_logging(client: Client) -> None:
    import logging

    from .logging_handler import PyloggleHandler

    for lg in [logging.getLogger()] + [logging.getLogger(n) for n in list(logging.Logger.manager.loggerDict)]:
        for h in getattr(lg, "handlers", []):
            if isinstance(h, PyloggleHandler):
                h.client = client


def get_client() -> Optional[Client]:
    return _client


def _resolve(client: Optional[Client]) -> Optional[Client]:
    return client if client is not None else _client


def capture_exception(
    exc: Optional[BaseException] = None,
    *,
    level: str = "error",
    handled: bool = True,
    mechanism: str = "capture_exception",
    message: Optional[str] = None,
    tags: Optional[Mapping[str, Any]] = None,
    extra: Optional[Mapping[str, Any]] = None,
    fingerprint: Optional[Sequence[str]] = None,
    logger: str = "pyloggle",
    client: Optional[Client] = None,
) -> Optional[str]:
    """Report an exception (defaults to the one being handled). Returns the event id."""
    c = _resolve(client)
    if exc is None:
        exc = sys.exc_info()[1]
    if c is None or exc is None:
        return None
    event_id = uuid.uuid4().hex
    ev: Dict[str, Any] = {
        "event_id": event_id,
        "ts": _scope.iso_now(),
        "level": normalize_level(level),
        "logger": logger,
        "message": message or (str(exc) or type(exc).__name__),
        "exception": exception_to_payload(exc, config=c.config),
        "mechanism": {"type": mechanism, "handled": bool(handled)},
        "attrs": {},
    }
    if tags:
        ev["tags"] = dict(tags)
    if extra:
        ev["extra"] = dict(extra)
    if fingerprint:
        ev["fingerprint"] = [str(p) for p in fingerprint]
    mark_captured(exc, event_id)
    c.emit(ev)
    return event_id


def capture_message(
    message: str,
    *,
    level: str = "info",
    logger: str = "pyloggle",
    tags: Optional[Mapping[str, Any]] = None,
    extra: Optional[Mapping[str, Any]] = None,
    fingerprint: Optional[Sequence[str]] = None,
    client: Optional[Client] = None,
) -> Optional[str]:
    c = _resolve(client)
    if c is None:
        return None
    event_id = uuid.uuid4().hex
    ev: Dict[str, Any] = {
        "event_id": event_id,
        "ts": _scope.iso_now(),
        "level": normalize_level(level),
        "logger": logger,
        "message": str(message),
        "attrs": {},
    }
    if tags:
        ev["tags"] = dict(tags)
    if extra:
        ev["extra"] = dict(extra)
    if fingerprint:
        ev["fingerprint"] = [str(p) for p in fingerprint]
    c.emit(ev)
    return event_id


def add_breadcrumb(
    message: str,
    *,
    category: str = "default",
    level: str = "info",
    type: str = "default",
    data: Optional[Mapping[str, Any]] = None,
) -> None:
    _scope.add_breadcrumb(message, category=category, level=normalize_level(level), type=type, data=dict(data) if data else None)


def set_user(user: Optional[Mapping[str, Any]], *, client: Optional[Client] = None) -> None:
    """Attach the current user. The id is hashed for impact counts; raw fields are only sent
    when the client is configured with send_default_pii=True."""
    if not user:
        _scope.set_user(None)
        return
    c = _resolve(client)
    u = dict(user)
    if u.get("id") is not None and not u.get("id_hash"):
        u["id_hash"] = hash_user_id(u["id"], salt=getattr(getattr(c, "config", None), "user_id_salt", None))
    _scope.set_user(u)


def set_tag(key: str, value: Any) -> None:
    _scope.set_tag(key, value)


def set_tags(tags: Mapping[str, Any]) -> None:
    _scope.set_tags(dict(tags))


def remove_tag(key: str) -> None:
    _scope.remove_tag(key)


def set_context(name: str, data: Optional[Mapping[str, Any]]) -> None:
    _scope.set_context(name, dict(data) if data is not None else None)


def set_release(release: Optional[str], *, client: Optional[Client] = None) -> None:
    c = _resolve(client)
    if c is not None:
        c.config.release = release


@contextmanager
def push_scope() -> Iterator[_scope.Scope]:
    """Temporarily isolate tags/user/breadcrumbs: changes inside the block do not leak out."""
    token = _scope.push_scope()
    try:
        yield _scope.effective_scope()
    finally:
        _scope.reset_scope(token)


def capture_errors(*, reraise: bool = True, level: str = "error", client: Optional[Client] = None) -> Callable:
    """Decorator reporting exceptions raised by sync or async callables."""

    def deco(fn: Callable) -> Callable:
        if inspect.iscoroutinefunction(fn):

            @functools.wraps(fn)
            async def aw(*a: Any, **kw: Any) -> Any:
                try:
                    return await fn(*a, **kw)
                except Exception as exc:
                    capture_exception(exc, level=level, handled=not reraise, mechanism="decorator", client=client)
                    if reraise:
                        raise
                    return None

            return aw

        @functools.wraps(fn)
        def w(*a: Any, **kw: Any) -> Any:
            try:
                return fn(*a, **kw)
            except Exception as exc:
                capture_exception(exc, level=level, handled=not reraise, mechanism="decorator", client=client)
                if reraise:
                    raise
                return None

        return w

    return deco


def install_asyncio_hook(loop: Optional[asyncio.AbstractEventLoop] = None, *, client: Optional[Client] = None) -> bool:
    from . import hooks

    return hooks.install_asyncio_hook(loop, client=_resolve(client))


def flush(timeout_s: float = 2.0) -> bool:
    c = _client
    return c.flush(timeout_s) if c is not None else True


def close() -> None:
    global _client
    c = _client
    _client = None
    if c is not None:
        c.close()
