from __future__ import annotations

import re
from typing import Any, Dict, Iterable, Optional, Tuple

REDACTED = "[REDACTED]"


def _norm_keys(v: Iterable[str]) -> set[str]:
    return {str(k).strip().lower() for k in v if str(k).strip()}


DEFAULT_VALUE_PATTERNS = (
    r"\b(?:Bearer|Basic)\s+[A-Za-z0-9\-._~+/]{8,}=*",  # Authorization values
    r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b",  # JWT
    r"\bsk_(?:live|test)_[A-Za-z0-9]{8,}\b",  # Stripe-style secret keys
    r"\bAKIA[0-9A-Z]{16}\b",  # AWS access key id
    r"\bxox[baprs]-[A-Za-z0-9-]{10,}\b",  # Slack tokens
)


class ScrubConfig:
    """
    Configuration for SDK-side PII/secret scrubbing before events are exported.

    - Key-based (exact match) plus regex key patterns, applied anywhere in the event.
    - Path-aware rules for http.headers / http.body.
    - Value patterns redact secret-looking strings (bearer tokens, JWTs, cloud keys) wherever
      they appear, except in source context lines.
    """

    def __init__(
        self,
        *,
        redacted: str = REDACTED,
        keys: Iterable[str] = (),
        key_patterns: Iterable[str] = (),
        header_keys: Iterable[str] = (),
        body_keys: Iterable[str] = (),
        value_patterns: Optional[Iterable[str]] = None,
    ) -> None:
        self.redacted = redacted
        self.keys = _norm_keys(keys)
        self.header_keys = _norm_keys(header_keys)
        self.body_keys = _norm_keys(body_keys)
        self.key_patterns = tuple(re.compile(p, re.IGNORECASE) for p in key_patterns if str(p).strip())
        vp = DEFAULT_VALUE_PATTERNS if value_patterns is None else tuple(value_patterns)
        self.value_patterns = tuple(re.compile(p) for p in vp if str(p).strip())

    def merged_with(self, extra_keys: Iterable[str]) -> "ScrubConfig":
        extra = _norm_keys(extra_keys or ())
        if not extra:
            return self
        m = ScrubConfig.__new__(ScrubConfig)
        m.redacted = self.redacted
        m.keys = set(self.keys) | extra
        m.header_keys = self.header_keys
        m.body_keys = self.body_keys
        m.key_patterns = self.key_patterns
        m.value_patterns = self.value_patterns
        return m


def default_scrub_config() -> ScrubConfig:
    # Conservative defaults: common header + body secrets.
    return ScrubConfig(
        header_keys=(
            "authorization",
            "proxy-authorization",
            "cookie",
            "set-cookie",
            "x-api-key",
            "x-forwarded-authorization",
        ),
        body_keys=(
            "password",
            "passwd",
            "passphrase",
            "secret",
            "token",
            "access_token",
            "refresh_token",
            "api_key",
            "apikey",
            "client_secret",
            "private_key",
        ),
        # Catch "something_token", "jwt", etc. Users can extend/override.
        key_patterns=(
            r".*token.*",
            r".*secret.*",
            r".*password.*",
            r".*api[-_]?key.*",
        ),
    )


def scrub_event(event: Dict[str, Any], scrub_keys: Iterable[str] | None = None, *, config: ScrubConfig | None = None) -> Dict[str, Any]:
    """
    Scrub secrets from an event dict.

    Backwards compatible:
    - If `scrub_keys` is provided, it is treated as additional exact-match keys.
    - Prefer passing `config=...` for richer control.
    """
    cfg = (config or default_scrub_config()).merged_with(scrub_keys or ())
    return _scrub_any(event, cfg, path=())


def _should_redact_key(cfg: ScrubConfig, key: str, *, path: Tuple[str, ...]) -> bool:
    k = key.lower()

    def _in_http_section(section: str) -> bool:
        for i in range(len(path) - 1):
            if path[i] == "http" and path[i + 1] == section:
                return True
        return False

    if _in_http_section("headers") and k in cfg.header_keys:
        return True
    if _in_http_section("body") and k in cfg.body_keys:
        return True
    if k in cfg.keys:
        return True
    for rx in cfg.key_patterns:
        if rx.fullmatch(k) or rx.search(k):
            return True
    return False


_NO_VALUE_SCRUB = {"context", "pre_context", "post_context"}


def _scrub_value(cfg: ScrubConfig, v: str, path: Tuple[str, ...]) -> str:
    if not cfg.value_patterns or len(v) < 8 or len(v) > 65536:
        return v
    if path and path[-1] in _NO_VALUE_SCRUB:
        return v
    out = v
    for rx in cfg.value_patterns:
        out = rx.sub(cfg.redacted, out)
    return out


def _scrub_any(v: Any, cfg: ScrubConfig, *, path: Tuple[str, ...]) -> Any:
    if isinstance(v, dict):
        out: Dict[str, Any] = {}
        for k, val in v.items():
            sk = str(k)
            if _should_redact_key(cfg, sk, path=path):
                out[k] = cfg.redacted
            else:
                out[k] = _scrub_any(val, cfg, path=path + (sk.lower(),))
        return out
    if isinstance(v, list):
        return [_scrub_any(x, cfg, path=path) for x in v]
    if isinstance(v, tuple):
        return tuple(_scrub_any(x, cfg, path=path) for x in v)
    if isinstance(v, str):
        return _scrub_value(cfg, v, path)
    return v
