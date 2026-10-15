"""Request counts: every request counted, none listed.

A request that gets no request line (``emit_request_latency`` is off, the default) is still
counted, for Analytics → Usage and the request charts on Dashboards. The framework integrations
call ``Client.count_request`` when a request ends; the requests are tallied here per minute and
endpoint, and the sender thread posts each tally as one line to ``<ingest_url>/requests`` once its
minute is over, or as soon as it holds ``CAP`` requests. A tally keeps every response time and
the distinct people (user id hashes, never raw ids), so nothing is sampled or estimated here.

Requests that crash are counted by their error event, and OpenTelemetry server spans by the
server, so the integrations leave those out: every request is counted once.
"""
from __future__ import annotations

import threading
import time
from datetime import datetime, timezone
from typing import Dict, List, Optional, Set, Tuple

# A tally is sent when it reaches this many requests, which bounds its memory and its line.
CAP = 1000
# Endpoints tallied at once. A real app has far fewer; past this, new endpoints wait for the
# next minute rather than growing without bound.
MAX_OPEN = 5000

_Key = Tuple[int, str, str]  # (minute as unix seconds, method, route)


class _Tally:
    __slots__ = ("requests", "errors", "durations", "users")

    def __init__(self) -> None:
        self.requests = 0
        self.errors = 0
        self.durations: List[float] = []
        self.users: Set[str] = set()


class RequestCounter:
    """Thread-safe tallies of requests per minute and endpoint."""

    def __init__(self, cap: int = CAP, max_open: int = MAX_OPEN) -> None:
        self._lock = threading.Lock()
        self._open: Dict[_Key, _Tally] = {}
        self._full: List[dict] = []
        self._cap = max(1, int(cap))
        self._max_open = max(1, int(max_open))
        self.dropped = 0

    def add(
        self,
        method: str,
        route: str,
        status: int,
        duration_ms: Optional[float] = None,
        user_id_hash: Optional[str] = None,
        now: Optional[float] = None,
    ) -> None:
        minute = int((time.time() if now is None else now) // 60) * 60
        key = (minute, str(method).upper(), str(route))
        with self._lock:
            tally = self._open.get(key)
            if tally is None:
                if len(self._open) >= self._max_open:
                    self.dropped += 1
                    return
                tally = self._open[key] = _Tally()
            tally.requests += 1
            if status >= 500:
                tally.errors += 1
            if duration_ms is not None and duration_ms > 0:
                tally.durations.append(round(float(duration_ms), 2))
            if user_id_hash:
                tally.users.add(str(user_id_hash))
            if tally.requests >= self._cap:
                self._full.append(_line(key, self._open.pop(key)))

    def take(self, now: Optional[float] = None, everything: bool = False) -> List[dict]:
        """The tallies ready to send: full ones, minutes that are over and, with ``everything``
        (flush, shutdown), the minute still running."""
        current = int((time.time() if now is None else now) // 60) * 60
        with self._lock:
            out, self._full = self._full, []
            for key in [k for k in self._open if everything or k[0] < current]:
                out.append(_line(key, self._open.pop(key)))
        return out

    def __len__(self) -> int:
        with self._lock:
            return len(self._open) + len(self._full)


def _line(key: _Key, tally: _Tally) -> dict:
    minute, method, route = key
    line: dict = {
        "minute": datetime.fromtimestamp(minute, tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "method": method,
        "route": route,
        "requests": tally.requests,
        "errors": tally.errors,
    }
    if tally.durations:
        line["durations_ms"] = tally.durations
    if tally.users:
        line["users"] = sorted(tally.users)
    return line


def counts_url(ingest_url: str) -> str:
    """Where tallies go: ``http://host:8080/v1/ingest`` -> ``http://host:8080/v1/ingest/requests``."""
    base, sep, query = str(ingest_url or "").partition("?")
    return base.rstrip("/") + "/requests" + (sep + query if sep else "")
