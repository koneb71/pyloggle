"""Shared request-context helpers for the framework integrations."""
from __future__ import annotations

import fnmatch
import uuid
from typing import Any, Callable, Dict, Mapping, Optional, Tuple

from . import _body
from . import scope as _scope
from .context import parse_traceparent

_SKIP_HEADERS = {"cookie", "authorization", "proxy-authorization", "x-api-key", "set-cookie"}


def request_identity(headers: Mapping[str, str], config: Any) -> Tuple[str, Optional[str], Optional[str]]:
    """(request_id, trace_id, span_id) honoring X-Request-ID / traceparent when trusted."""
    trust = bool(getattr(config, "trust_incoming_request_id", True))
    rid = None
    if trust:
        rid = headers.get("x-request-id") or headers.get("x-correlation-id") or None
        if rid:
            rid = str(rid).strip()[:128]
    if not rid:
        rid = uuid.uuid4().hex
    trace_id = span_id = None
    if trust:
        trace_id, span_id = parse_traceparent(headers.get("traceparent"))
    return rid, trace_id, span_id


def client_ip(remote_addr: str, headers: Mapping[str, str], config: Any) -> str:
    if bool(getattr(config, "trust_proxy_headers", False)):
        xff = headers.get("x-forwarded-for")
        if xff:
            return xff.split(",")[0].strip()
        if headers.get("x-real-ip"):
            return headers["x-real-ip"].strip()
    return remote_addr or ""


def headers_for_event(headers: Mapping[str, str], config: Any) -> Dict[str, str]:
    """Lower-cased headers capped in count/length; secret headers are dropped outright
    (the scrubber also redacts them, this just avoids carrying them around)."""
    if not bool(getattr(config, "capture_request_headers", True)):
        return {}
    limit = int(getattr(config, "max_request_headers", 50) or 50)
    out: Dict[str, str] = {}
    for k, v in headers.items():
        lk = str(k).lower()
        if lk in _SKIP_HEADERS:
            out[lk] = "[REDACTED]"
            continue
        if len(out) >= limit:
            break
        out[lk] = str(v)[:1024]
    return out


BodyReader = Callable[[], Tuple[Any, bool]]


def request_details(config: Any, headers: Mapping[str, str], read_body: BodyReader) -> Callable[[], Tuple[Dict[str, Any], bool]]:
    """What an error raised inside the request adds to the scope's http block: the scrubbed
    headers and the body, read only if such an error happens."""

    def details() -> Tuple[Dict[str, Any], bool]:
        out: Dict[str, Any] = {}
        hdrs = headers_for_event(headers, config)
        if hdrs:
            out["headers"] = hdrs
        body, truncated = read_body() if _body.enabled(config) else (None, False)
        if body is not None:
            out["body"] = body
        return out, truncated

    return details


def begin_request(
    *,
    config: Any,
    headers: Mapping[str, str],
    http: Dict[str, Any],
    read_body: Optional[BodyReader] = None,
) -> Tuple[Any, str]:
    """Push a fresh request scope with identity + http context. Returns (token, request_id).
    ``read_body`` lets errors logged inside the request carry its body."""
    token = _scope.push_scope(inherit=True)
    sc = _scope.effective_scope()
    rid, trace_id, span_id = request_identity(headers, config)
    sc.request_id = rid
    if trace_id:
        sc.trace_id = trace_id
        sc.span_id = span_id
    sc.http = dict(http)
    sc.request_details = request_details(config, headers, read_body) if read_body is not None else None
    sc.transaction = str(http.get("route") or "") or None
    _scope.add_breadcrumb(
        f"{http.get('method', '')} {http.get('route') or http.get('url') or ''}".strip(),
        category="http.request",
        type="http",
        data={k: http[k] for k in ("method", "url") if http.get(k)},
    )
    return token, rid


def ignored_request(config: Any, method: str, path: str, route: str = "", status: int = 0) -> bool:
    """Whether this request should get no request line (``ignore_requests``). A failed (5xx)
    request is never ignored: its line is how a failure without an exception gets seen."""
    if status >= 500:
        return False
    method = (method or "").upper()
    for rule in getattr(config, "ignore_requests", ()) or ():
        verb, _, glob = str(rule).strip().partition(" ")
        if not glob:
            verb, glob = "*", verb
        glob = glob.strip()
        if verb != "*" and verb.upper() != method:
            continue
        if fnmatch.fnmatchcase(path or "", glob) or (route and fnmatch.fnmatchcase(route, glob)):
            return True
    return False


def count_request(
    client: Any,
    method: str,
    path: str,
    route: str,
    status: int,
    duration_ms: Optional[float] = None,
    user_id_hash: Optional[str] = None,
) -> None:
    """Count a finished request that gets no request line, unless ``ignore_requests`` matches it
    or it matched no route (unknown paths would make every typo an endpoint). Callers leave out
    requests that crashed (their error event counts them) and OpenTelemetry server spans (the
    server counts those)."""
    count = getattr(client, "count_request", None)
    if count is None or not route:
        return
    if ignored_request(client.config, method, path, route, status):
        return
    count(method, route, status, duration_ms, user_id_hash)


def end_request(token: Any) -> None:
    _scope.reset_scope(token)
