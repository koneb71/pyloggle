from __future__ import annotations

import linecache
import os
import sys
import traceback
from typing import Any, Dict, List, Optional, Tuple


def exception_to_payload(
    exc: BaseException,
    *,
    in_app_prefixes: Optional[List[str]] = None,
) -> Dict[str, Any]:
    etype = type(exc).__name__
    emsg = str(exc)

    frames: List[Dict[str, Any]] = []
    tb = exc.__traceback__
    extracted = traceback.extract_tb(tb) if tb else []

    for fr in extracted:
        filename = fr.filename
        lineno = int(fr.lineno or 0)
        func = fr.name or ""
        context = _getline(filename, lineno)
        in_app = is_in_app_file(filename, in_app_prefixes=in_app_prefixes)
        frames.append(
            {
                "filename": filename,
                "lineno": lineno,
                "function": func,
                "context": context,
                "in_app": bool(in_app),
            }
        )

    return {
        "type": etype,
        "message": emsg,
        "frames": frames,
    }


def is_in_app_file(filename: str, *, in_app_prefixes: Optional[List[str]] = None) -> bool:
    if not filename:
        return False
    fn = filename.replace("\\", "/")

    # Respect explicit include prefixes if provided.
    if in_app_prefixes:
        for p in in_app_prefixes:
            if p and fn.startswith(p.replace("\\", "/")):
                return True

    # Heuristic: exclude stdlib + site-packages + venv.
    stdlib = (getattr(sys, "base_prefix", None) or sys.prefix).replace("\\", "/")
    if fn.startswith(stdlib + "/lib/"):
        return False
    lowered = fn.lower()
    if "/site-packages/" in lowered or "/dist-packages/" in lowered:
        return False
    if "/.venv/" in lowered or "/venv/" in lowered:
        return False
    return True


def _getline(filename: str, lineno: int) -> str:
    if not filename or lineno <= 0:
        return ""
    try:
        line = linecache.getline(filename, lineno)
        return line.strip()
    except Exception:
        return ""


