"""Outgoing HTTP breadcrumbs for requests, httpx and urllib3: one ``http.client`` crumb per
call with method, URL (no query string, no credentials), status and duration; failures leave an
error crumb. Only libraries that are installed get patched, and patching is idempotent."""
from __future__ import annotations

import contextvars
import functools
import time
from typing import Any, Dict, Optional
from urllib.parse import urlsplit, urlunsplit

from .. import scope as _scope

_patched: Dict[str, Any] = {}  # name -> original callable
_depth: contextvars.ContextVar[int] = contextvars.ContextVar("pyloggle_http_depth", default=0)
_MAX_URL = 512


def clean_url(url: Any) -> str:
    """Scheme, host and path only: query strings and userinfo carry secrets."""
    if url is None or url == "":
        return ""
    try:
        parts = urlsplit(str(url))
    except Exception:
        return ""
    host = parts.hostname or ""
    if parts.port and host:
        host = f"{host}:{parts.port}"
    if ":" in (parts.hostname or "") and host.startswith(parts.hostname or ""):
        host = f"[{parts.hostname}]" + (f":{parts.port}" if parts.port else "")
    return urlunsplit((parts.scheme, host, parts.path, "", ""))[:_MAX_URL]


def _crumb(method: str, url: str, start: float, *, status: Optional[int] = None, error: Optional[BaseException] = None) -> None:
    data: Dict[str, Any] = {"method": (method or "").upper(), "url": url, "duration_ms": round((time.perf_counter() - start) * 1000, 2)}
    level = "info"
    if status is not None:
        data["status_code"] = int(status)
        if status >= 500:
            level = "error"
        elif status >= 400:
            level = "warning"
    if error is not None:
        data["error"] = f"{type(error).__name__}: {str(error)[:200]}"
        level = "error"
    try:
        _scope.add_breadcrumb(f"{data['method']} {url}".strip(), category="http.client", type="http", level=level, data=data)
    except Exception:
        pass


def _wrap(name: str, fn, method_of, url_of, status_of):
    """Patch fn so one call leaves one crumb, however many wrapped layers it goes through."""

    @functools.wraps(fn)
    def wrapper(*args, **kwargs):
        if _depth.get() > 0:
            return fn(*args, **kwargs)
        token = _depth.set(1)
        start = time.perf_counter()
        method, url = "", ""
        try:
            method, url = method_of(*args, **kwargs), clean_url(url_of(*args, **kwargs))
        except Exception:
            pass
        try:
            resp = fn(*args, **kwargs)
        except BaseException as exc:
            _depth.reset(token)
            _crumb(method, url, start, error=exc if isinstance(exc, Exception) else None)
            raise
        _depth.reset(token)
        try:
            _crumb(method, url, start, status=status_of(resp))
        except Exception:
            _crumb(method, url, start)
        return resp

    wrapper._pyloggle_patch = name  # type: ignore[attr-defined]
    return wrapper


def _wrap_async(name: str, fn, method_of, url_of, status_of):
    @functools.wraps(fn)
    async def wrapper(*args, **kwargs):
        if _depth.get() > 0:
            return await fn(*args, **kwargs)
        token = _depth.set(1)
        start = time.perf_counter()
        method, url = "", ""
        try:
            method, url = method_of(*args, **kwargs), clean_url(url_of(*args, **kwargs))
        except Exception:
            pass
        try:
            resp = await fn(*args, **kwargs)
        except BaseException as exc:
            _depth.reset(token)
            _crumb(method, url, start, error=exc if isinstance(exc, Exception) else None)
            raise
        _depth.reset(token)
        try:
            _crumb(method, url, start, status=status_of(resp))
        except Exception:
            _crumb(method, url, start)
        return resp

    wrapper._pyloggle_patch = name  # type: ignore[attr-defined]
    return wrapper


def _patch(owner: Any, attr: str, name: str, wrap, method_of, url_of, status_of) -> bool:
    fn = getattr(owner, attr, None)
    if fn is None or getattr(fn, "_pyloggle_patch", None) == name:
        return False
    setattr(owner, attr, wrap(name, fn, method_of, url_of, status_of))
    _patched[name] = (owner, attr, fn)
    return True


def install() -> list:
    """Patch every supported client library that is installed. Returns what was patched."""
    done = []
    try:
        import requests  # type: ignore

        if _patch(requests.Session, "send", "requests", _wrap,
                  lambda self, req, *a, **k: getattr(req, "method", ""), lambda self, req, *a, **k: getattr(req, "url", ""),
                  lambda r: getattr(r, "status_code", None)):
            done.append("requests")
    except Exception:
        pass
    try:
        import httpx  # type: ignore

        if _patch(httpx.Client, "send", "httpx", _wrap,
                  lambda self, req, *a, **k: getattr(req, "method", ""), lambda self, req, *a, **k: getattr(req, "url", ""),
                  lambda r: getattr(r, "status_code", None)):
            done.append("httpx")
        if _patch(httpx.AsyncClient, "send", "httpx.async", _wrap_async,
                  lambda self, req, *a, **k: getattr(req, "method", ""), lambda self, req, *a, **k: getattr(req, "url", ""),
                  lambda r: getattr(r, "status_code", None)):
            done.append("httpx.async")
    except Exception:
        pass
    try:
        import urllib3  # type: ignore

        def _u3_url(self, method, url, *a, **k):
            scheme = getattr(self, "scheme", "http")
            host = getattr(self, "host", "") or ""
            port = getattr(self, "port", None)
            if port and port not in (80, 443):
                host = f"{host}:{port}"
            return f"{scheme}://{host}{url if str(url).startswith('/') else '/' + str(url)}" if host else str(url)

        if _patch(urllib3.HTTPConnectionPool, "urlopen", "urllib3", _wrap,
                  lambda self, method, url, *a, **k: method, _u3_url, lambda r: getattr(r, "status", None)):
            done.append("urllib3")
    except Exception:
        pass
    return done


def uninstall() -> None:
    for name, (owner, attr, fn) in list(_patched.items()):
        try:
            setattr(owner, attr, fn)
        except Exception:
            pass
        _patched.pop(name, None)
