from __future__ import annotations

import atexit
import json
import queue
import threading
import time
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Dict, Iterable, Optional

from .scrub import ScrubConfig, default_scrub_config, scrub_event


@dataclass
class ClientConfig:
    ingest_url: str = "http://localhost:8080/v1/ingest"
    api_key: Optional[str] = None  # sent as X-API-Key

    service: str = "unknown"
    env: str = "prod"
    project_id: str = "default"

    # user impact (hashed to avoid PII). If provided, used as an HMAC key when hashing user ids.
    user_id_salt: Optional[str] = None

    # resource control
    max_queue_size: int = 10_000
    flush_interval_s: float = 1.0
    batch_size: int = 200
    request_timeout_s: float = 2.0

    # scrubbing
    # Preferred: full scrub config (header/body rules + regex patterns).
    scrub: ScrubConfig = field(default_factory=default_scrub_config)

    # Backwards compatibility: additional exact-match keys to redact anywhere in the event.
    scrub_keys: Iterable[str] = ()


class Client:
    def __init__(self, config: ClientConfig):
        self.config = config
        self._q: queue.Queue[dict] = queue.Queue(maxsize=max(1, int(config.max_queue_size)))
        self._stop = threading.Event()
        self._t = threading.Thread(target=self._run, name="pyloggle-sender", daemon=True)
        self._t.start()
        atexit.register(self.close)

    def emit(self, event: Dict[str, Any]) -> None:
        # Fill defaults
        event.setdefault("service", self.config.service)
        event.setdefault("env", self.config.env)
        event.setdefault("project_id", self.config.project_id)

        # Scrub sensitive keys before enqueueing
        event = scrub_event(event, scrub_keys=self.config.scrub_keys, config=self.config.scrub)

        try:
            self._q.put_nowait(event)
        except queue.Full:
            # Always try harder for errors; otherwise drop silently.
            level = str(event.get("level", "")).lower()
            is_err = level == "error" or bool(event.get("exception"))
            if not is_err:
                return
            try:
                self._q.put(event, timeout=0.25)
            except queue.Full:
                return

    def flush(self, timeout_s: float = 2.0) -> None:
        deadline = time.time() + timeout_s
        while time.time() < deadline and not self._q.empty():
            time.sleep(0.01)

    def close(self) -> None:
        if self._stop.is_set():
            return
        self._stop.set()
        try:
            self._t.join(timeout=2.0)
        except Exception:
            pass

    def _run(self) -> None:
        buf: list[dict] = []
        last = time.time()

        while not self._stop.is_set():
            timeout = max(0.01, self.config.flush_interval_s - (time.time() - last))
            try:
                item = self._q.get(timeout=timeout)
                buf.append(item)
                if len(buf) >= self.config.batch_size:
                    self._send(buf)
                    buf.clear()
                    last = time.time()
            except queue.Empty:
                if buf:
                    self._send(buf)
                    buf.clear()
                last = time.time()

        # final drain
        try:
            while True:
                buf.append(self._q.get_nowait())
                if len(buf) >= self.config.batch_size:
                    self._send(buf)
                    buf.clear()
        except queue.Empty:
            if buf:
                self._send(buf)

    def _send(self, events: list[dict]) -> None:
        # NDJSON payload
        payload = "\n".join(json.dumps(e, separators=(",", ":"), default=str) for e in events).encode("utf-8")

        req = urllib.request.Request(self.config.ingest_url, data=payload, method="POST")
        req.add_header("Content-Type", "application/x-ndjson")
        if self.config.api_key:
            req.add_header("X-API-Key", self.config.api_key)

        try:
            with urllib.request.urlopen(req, timeout=self.config.request_timeout_s) as _:
                return
        except Exception:
            # best-effort; drop on failure
            return


