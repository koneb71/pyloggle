from __future__ import annotations

import atexit
import gzip
import json
import logging
import os
import platform
import queue
import random
import socket
import sys
import threading
import time
import urllib.error
import urllib.request
import uuid
from dataclasses import dataclass, field
from typing import Any, Dict, Iterable, Optional, Sequence

from . import scope as _scope
from ._version import SDK_NAME, __version__
from .scrub import ScrubConfig, default_scrub_config, scrub_event
from .stack import current_thread_name

_log = logging.getLogger("pyloggle.sdk")

_LEVELS = {"warning": "warn", "critical": "fatal", "err": "error", "trace": "debug", "notice": "info"}


def normalize_level(level: Any) -> str:
    l = str(level or "info").strip().lower()
    return _LEVELS.get(l, l)


@dataclass
class ClientConfig:
    ingest_url: str = "http://localhost:8080/v1/ingest"
    api_key: Optional[str] = None  # sent as X-API-Key

    service: str = "unknown"
    env: str = "prod"
    project_id: str = "default"
    release: Optional[str] = None
    hostname: Optional[str] = None  # auto-detected when None

    # per-request telemetry (opt-in to avoid extra noise/cost)
    emit_request_latency: bool = False

    # request body capture (opt-in; size-limited)
    emit_request_body: bool = False
    max_request_body_bytes: int = 4096

    # request headers are captured (scrubbed) on error events
    capture_request_headers: bool = True
    max_request_headers: int = 50
    trust_incoming_request_id: bool = True  # honor X-Request-ID / traceparent from upstream
    trust_proxy_headers: bool = False  # take client_ip from X-Forwarded-For

    # user impact (hashed to avoid PII). If provided, used as an HMAC key when hashing user ids.
    user_id_salt: Optional[str] = None
    # raw user id/email/username/ip are only sent when True (id_hash is always sent)
    send_default_pii: bool = False

    # exception capture
    with_locals: bool = True
    locals_scope: str = "in_app"  # in_app | all | none
    context_lines: int = 5
    max_frames: int = 100
    max_chain: int = 5
    max_locals: int = 20
    max_local_repr: int = 256
    in_app_include: Sequence[str] = ()  # module or path prefixes that are "your code"
    in_app_exclude: Sequence[str] = ()
    max_breadcrumbs: int = 50
    install_excepthooks: bool = True  # sys/threading/unraisable (+ asyncio via integrations)

    # resource control
    max_queue_size: int = 10_000
    flush_interval_s: float = 1.0
    batch_size: int = 200
    request_timeout_s: float = 2.0
    shutdown_timeout_s: float = 2.0
    max_event_bytes: int = 1_000_000

    # transport
    gzip: bool = True
    gzip_min_bytes: int = 1024
    max_retries: int = 3
    retry_backoff_s: float = 0.5
    debug: bool = False

    tags: Dict[str, str] = field(default_factory=dict)

    # scrubbing
    scrub: ScrubConfig = field(default_factory=default_scrub_config)
    # Backwards compatibility: additional exact-match keys to redact anywhere in the event.
    scrub_keys: Iterable[str] = ()


class _Flush:
    """Queue sentinel: send whatever is buffered now."""


class _Stop:
    """Queue sentinel: drain and exit."""


class Client:
    def __init__(self, config: ClientConfig):
        self.config = config
        self._q: "queue.Queue[Any]" = queue.Queue(maxsize=max(1, int(config.max_queue_size)))
        self._stop = threading.Event()
        self._scrub_cfg = (config.scrub or default_scrub_config()).merged_with(config.scrub_keys or ())
        self._runtime = _runtime_info()
        self._hostname = config.hostname or _hostname()
        self._warned_4xx = False
        self._t = threading.Thread(target=self._run, name="pyloggle-sender", daemon=True)
        self._t.start()
        atexit.register(self.close)
        if getattr(config, "install_excepthooks", True):
            try:
                from . import hooks

                hooks.install(self)
            except Exception:
                pass

    # --- public --------------------------------------------------------------------------

    def emit(self, event: Dict[str, Any]) -> None:
        """Enrich, scrub and enqueue one event (same entry point as 0.1)."""
        try:
            event = self._enrich(event)
            event = scrub_event(event, config=self._scrub_cfg)
            event = self._bound(event)
        except Exception:
            if self.config.debug:
                _log.exception("pyloggle: enrich failed")
            return
        try:
            self._q.put_nowait(event)
        except queue.Full:
            level = str(event.get("level", "")).lower()
            is_err = level in ("error", "fatal") or bool(event.get("exception"))
            if not is_err:
                return
            try:
                self._q.put(event, timeout=0.25)
            except queue.Full:
                return

    capture_event = emit

    def flush(self, timeout_s: float = 2.0) -> bool:
        """Send everything queued so far, including the in-flight batch. Returns True if done in time."""
        if self._stop.is_set():
            return True
        try:
            self._q.put_nowait(_Flush())
        except queue.Full:
            pass
        deadline = time.monotonic() + max(0.0, timeout_s)
        with self._q.all_tasks_done:
            while self._q.unfinished_tasks:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return False
                self._q.all_tasks_done.wait(remaining)
        return True

    def close(self) -> None:
        if self._stop.is_set():
            return
        self._stop.set()
        try:
            self._q.put_nowait(_Stop())
        except queue.Full:
            pass
        try:
            self._t.join(timeout=float(getattr(self.config, "shutdown_timeout_s", 2.0)))
        except Exception:
            pass

    # --- enrichment ------------------------------------------------------------------------

    def _enrich(self, ev: Dict[str, Any]) -> Dict[str, Any]:
        cfg = self.config
        ev.setdefault("service", cfg.service)
        ev.setdefault("env", cfg.env)
        ev.setdefault("project_id", cfg.project_id)
        if not ev.get("event_id"):
            ev["event_id"] = uuid.uuid4().hex
        if not ev.get("ts"):
            ev["ts"] = _scope.iso_now()
        ev["level"] = normalize_level(ev.get("level"))
        if cfg.release and not ev.get("release"):
            ev["release"] = cfg.release
        if self._hostname and not ev.get("hostname"):
            ev["hostname"] = self._hostname
        ev.setdefault("sdk", {"name": SDK_NAME, "version": __version__})
        rt = dict(self._runtime)
        rt["thread"] = current_thread_name()
        ev.setdefault("runtime", rt)

        sc = _scope.effective_scope()
        gl = _scope.global_scope()
        if not ev.get("request_id") and sc.request_id:
            ev["request_id"] = sc.request_id
        if not ev.get("trace_id"):
            tid = sc.trace_id or _otel_trace_id()
            if tid:
                ev["trace_id"] = tid
        if not ev.get("span_id") and sc.span_id:
            ev["span_id"] = sc.span_id
        if not ev.get("transaction") and sc.transaction:
            ev["transaction"] = sc.transaction
        user = sc.user or gl.user
        if user and not ev.get("user"):
            ev["user"] = self._user_payload(user)
        tags: Dict[str, str] = {}
        tags.update({str(k): _scope._stringify(v) for k, v in (cfg.tags or {}).items()})
        tags.update(gl.tags)
        if sc is not gl:
            tags.update(sc.tags)
        if ev.get("tags"):
            tags.update({str(k): _scope._stringify(v) for k, v in dict(ev["tags"]).items()})
        if tags:
            ev["tags"] = tags
        extra: Dict[str, Any] = {}
        extra.update(gl.extra)
        if sc is not gl:
            extra.update(sc.extra)
        if isinstance(ev.get("extra"), dict):
            extra.update(ev["extra"])
        if extra:
            ev["extra"] = extra
        if sc.http and not ev.get("http"):
            ev["http"] = dict(sc.http)

        is_err = ev["level"] in ("error", "fatal") or bool(ev.get("exception"))
        if is_err and not ev.get("breadcrumbs") and int(cfg.max_breadcrumbs or 0) > 0:
            crumbs = list(_scope.iter_breadcrumbs(int(cfg.max_breadcrumbs)))
            own = ev.get("event_id")
            crumbs = [c for c in crumbs if c.get("data", {}).get("event_id") != own]
            if crumbs:
                ev["breadcrumbs"] = crumbs

        attrs = ev.get("attrs")
        if isinstance(attrs, dict):
            ev["attrs"] = {str(k): _attr_str(v) for k, v in attrs.items() if v is not None}
        elif attrs is None:
            ev["attrs"] = {}
        return ev

    def _user_payload(self, user: Dict[str, Any]) -> Dict[str, Any]:
        out: Dict[str, Any] = {}
        if user.get("id_hash"):
            out["id_hash"] = str(user["id_hash"])
        elif user.get("id") is not None:
            from .user import hash_user_id

            out["id_hash"] = hash_user_id(user["id"], salt=self.config.user_id_salt)
        if self.config.send_default_pii:
            for k in ("id", "email", "username", "ip"):
                if user.get(k) not in (None, ""):
                    out[k] = str(user[k])
        return out

    def _bound(self, ev: Dict[str, Any]) -> Dict[str, Any]:
        """Keep an event under max_event_bytes by shedding the least valuable parts first."""
        limit = int(self.config.max_event_bytes or 0)
        if limit <= 0:
            return ev
        size = len(json.dumps(ev, separators=(",", ":"), default=str))
        if size <= limit:
            return ev
        exc = ev.get("exception")
        if isinstance(exc, dict):
            for fr in exc.get("frames") or []:
                fr.pop("vars", None)
            for link in exc.get("chain") or []:
                for fr in link.get("frames") or []:
                    fr.pop("vars", None)
        if len(json.dumps(ev, separators=(",", ":"), default=str)) <= limit:
            return ev
        ev.pop("breadcrumbs", None)
        if len(json.dumps(ev, separators=(",", ":"), default=str)) <= limit:
            return ev
        if isinstance(ev.get("http"), dict):
            ev["http"].pop("body", None)
        if len(json.dumps(ev, separators=(",", ":"), default=str)) <= limit:
            return ev
        msg = str(ev.get("message", ""))
        without = len(json.dumps({**ev, "message": ""}, separators=(",", ":"), default=str))
        room = max(256, limit - without - 64)
        if len(msg) > room:
            ev["message"] = msg[:room] + "…"
        ev.setdefault("attrs", {})["pyloggle.truncated"] = "true"
        return ev

    # --- sender thread ---------------------------------------------------------------------

    def _run(self) -> None:
        buf: list = []
        last = time.monotonic()

        def send_buf() -> None:
            if not buf:
                return
            try:
                self._send(list(buf))
            finally:
                for _ in buf:
                    self._q.task_done()
                buf.clear()

        while True:
            timeout = max(0.01, self.config.flush_interval_s - (time.monotonic() - last))
            try:
                item = self._q.get(timeout=timeout)
            except queue.Empty:
                send_buf()
                last = time.monotonic()
                continue
            if isinstance(item, _Stop):
                send_buf()
                self._q.task_done()
                break
            if isinstance(item, _Flush):
                send_buf()
                self._q.task_done()
                last = time.monotonic()
                continue
            buf.append(item)
            if len(buf) >= self.config.batch_size:
                send_buf()
                last = time.monotonic()

        # final drain after stop
        while True:
            try:
                item = self._q.get_nowait()
            except queue.Empty:
                break
            if isinstance(item, (_Stop, _Flush)):
                self._q.task_done()
                continue
            buf.append(item)
            if len(buf) >= self.config.batch_size:
                send_buf()
        send_buf()

    def _send(self, events: list) -> bool:
        payload = "\n".join(json.dumps(e, separators=(",", ":"), default=str) for e in events).encode("utf-8")
        headers = {
            "Content-Type": "application/x-ndjson",
            "User-Agent": f"{SDK_NAME}/{__version__}",
            "X-Pyloggle-SDK": f"python/{__version__}",
        }
        if self.config.api_key:
            headers["X-API-Key"] = self.config.api_key
        body = payload
        if self.config.gzip and len(payload) >= int(self.config.gzip_min_bytes or 0):
            body = gzip.compress(payload, compresslevel=6)
            headers["Content-Encoding"] = "gzip"

        attempts = max(1, int(self.config.max_retries) + 1)
        backoff = float(self.config.retry_backoff_s or 0.5)
        for attempt in range(attempts):
            try:
                req = urllib.request.Request(self.config.ingest_url, data=body, method="POST", headers=headers)
                with urllib.request.urlopen(req, timeout=self.config.request_timeout_s):
                    return True
            except urllib.error.HTTPError as e:
                status = int(getattr(e, "code", 0) or 0)
                if status == 413 and len(events) > 1:
                    half = len(events) // 2
                    return self._send(events[:half]) and self._send(events[half:])
                retry = status == 429 or status >= 500
                if not retry:
                    self._drop(f"server rejected batch with HTTP {status}")
                    return False
                wait = _retry_after(e) or (backoff * (2**attempt))
            except (urllib.error.URLError, socket.timeout, OSError, ConnectionError) as e:
                wait = backoff * (2**attempt)
                if attempt == attempts - 1:
                    self._drop(f"connection failed: {e}")
            except Exception as e:  # never let the sender die
                self._drop(f"unexpected send error: {e}")
                return False
            if attempt < attempts - 1 and not self._stop.is_set():
                time.sleep(min(wait, 10.0) + random.uniform(0, 0.1))
        return False

    def _drop(self, why: str) -> None:
        if self.config.debug:
            _log.warning("pyloggle: dropping batch: %s", why)
        elif not self._warned_4xx:
            self._warned_4xx = True
            _log.warning("pyloggle: dropping batch: %s (further drops are silent; set debug=True)", why)


def _retry_after(e: urllib.error.HTTPError) -> Optional[float]:
    try:
        v = e.headers.get("Retry-After") if e.headers else None
        if v:
            return min(float(v), 5.0)
    except Exception:
        pass
    return None


def _attr_str(v: Any) -> str:
    if isinstance(v, str):
        return v
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, (int, float)):
        return str(v)
    if isinstance(v, (dict, list, tuple)):
        try:
            return json.dumps(v, separators=(",", ":"), default=str)
        except Exception:
            return str(v)
    return str(v)


def _runtime_info() -> Dict[str, Any]:
    try:
        impl = platform.python_implementation()
    except Exception:
        impl = "python"
    try:
        plat = platform.platform()
    except Exception:
        plat = sys.platform
    return {"name": impl, "version": platform.python_version(), "platform": plat, "pid": os.getpid()}


def _hostname() -> str:
    try:
        return socket.gethostname()
    except Exception:
        return ""


def _otel_trace_id() -> Optional[str]:
    try:
        from .otel import current_trace_id

        return current_trace_id()
    except Exception:
        return None
