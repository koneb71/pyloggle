from __future__ import annotations

import re
from typing import Any, Dict, Iterable


REDACTED = "[REDACTED]"


def _norm_keys(v: Iterable[str]) -> set[str]:
    return {str(k).strip().lower() for k in v if str(k).strip()}


class ScrubConfig:
    """
    Configuration for SDK-side PII/secret scrubbing before events are exported.

    Notes:
    - Scrubbing is key-based (exact match) plus optional regex matching.
    - Scrubbing is path-aware for common HTTP shapes:
        - event["http"]["headers"] uses header rules
        - event["http"]["body"] uses body rules
      Everywhere else uses `keys`/`key_patterns`.
    """

    def __init__(
        self,
        *,
        redacted: str = REDACTED,
        keys: Iterable[str] = (),
        key_patterns: Iterable[str] = (),
        header_keys: Iterable[str] = (),
        body_keys: Iterable[str] = (),
    ) -> None:
        self.redacted = redacted
        self.keys = _norm_keys(keys)
        self.header_keys = _norm_keys(header_keys)
        self.body_keys = _norm_keys(body_keys)
        self.key_patterns = tuple(re.compile(p, re.IGNORECASE) for p in key_patterns if str(p).strip())


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
    cfg = config or default_scrub_config()
    extra = _norm_keys(scrub_keys or ())
    # Merge legacy keys into general key set
    merged = ScrubConfig(
        redacted=cfg.redacted,
        keys=tuple(cfg.keys | extra),
        key_patterns=tuple(p.pattern for p in cfg.key_patterns),
        header_keys=tuple(cfg.header_keys),
        body_keys=tuple(cfg.body_keys),
    )
    return _scrub_any(event, merged, path=())


def _should_redact_key(cfg: ScrubConfig, key: str, *, path: tuple[str, ...]) -> bool:
    k = key.lower()

    # Path-aware rules for common HTTP shapes.
    # Apply within (not just directly under) http.headers / http.body.
    def _in_http_section(section: str) -> bool:
        for i in range(len(path) - 1):
            if path[i] == "http" and path[i + 1] == section:
                return True
        return False

    if _in_http_section("headers"):
        if k in cfg.header_keys:
            return True
    if _in_http_section("body"):
        if k in cfg.body_keys:
            return True

    # General rules (apply anywhere)
    if k in cfg.keys:
        return True
    for rx in cfg.key_patterns:
        if rx.fullmatch(k) or rx.search(k):
            return True
    return False


def _scrub_any(v: Any, cfg: ScrubConfig, *, path: tuple[str, ...]) -> Any:
    if isinstance(v, dict):
        out: dict[str, Any] = {}
        for k, val in v.items():
            sk = str(k)
            if _should_redact_key(cfg, sk, path=path):
                out[k] = cfg.redacted
            else:
                out[k] = _scrub_any(val, cfg, path=path + (str(k).lower(),))
        return out
    if isinstance(v, list):
        return [_scrub_any(x, cfg, path=path) for x in v]
    if isinstance(v, tuple):
        return tuple(_scrub_any(x, cfg, path=path) for x in v)
    return v


