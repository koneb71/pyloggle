from __future__ import annotations

import hashlib
import hmac
from typing import Any, Optional


def hash_user_id(user_id: Any, *, salt: Optional[str] = None) -> str:
    """
    Hash a user identifier for user-impact metrics (unique affected users).

    - Never send/store the raw user id in events.
    - If `salt` is provided, uses HMAC-SHA256(salt, user_id) for better privacy.
    - Otherwise uses SHA256(user_id).
    """
    if user_id is None:
        return ""
    s = str(user_id)
    if not s:
        return ""
    msg = s.encode("utf-8")

    if salt:
        key = str(salt).encode("utf-8")
        return hmac.new(key, msg, hashlib.sha256).hexdigest()
    return hashlib.sha256(msg).hexdigest()


