"""Structured exception capture: frames with source context and locals, chained causes,
exception groups and notes. Frames are oldest-first (Python order); the crash site is last."""
from __future__ import annotations

import builtins
import linecache
import os
import re
import reprlib
import sys
import threading
import time
import traceback
from typing import Any, Dict, Iterable, List, Optional, Sequence, Tuple

_DEFAULTS = {
    "with_locals": True,
    "locals_scope": "in_app",  # in_app | all | none
    "context_lines": 5,
    "max_frames": 100,
    "max_chain": 5,
    "max_locals": 20,
    "max_local_repr": 256,
    "in_app_include": (),
    "in_app_exclude": (),
}

_GROUP_CLS = getattr(builtins, "BaseExceptionGroup", None)  # 3.11+

_SECRET_NAME = re.compile(r"(?i)(password|passwd|secret|token|api[-_]?key|authorization|cookie|private[-_]?key|credential)")


def _opt(config: Any, name: str) -> Any:
    v = getattr(config, name, None) if config is not None else None
    return _DEFAULTS[name] if v is None else v


def exception_to_payload(
    exc: BaseException,
    *,
    in_app_prefixes: Optional[List[str]] = None,
    config: Any = None,
) -> Dict[str, Any]:
    """Serialize an exception (same name as 0.1; richer output)."""
    seen: set = set()
    payload = _one(exc, in_app_prefixes, config, seen)
    chain: List[Dict[str, Any]] = []
    cur = exc
    for _ in range(int(_opt(config, "max_chain"))):
        nxt, relation = _next_in_chain(cur)
        if nxt is None or id(nxt) in seen:
            break
        link = _one(nxt, in_app_prefixes, config, seen)
        link["is_cause"] = relation == "cause"
        link["relation"] = relation
        chain.append(link)
        cur = nxt
    if chain:
        payload["chain"] = chain
    if _GROUP_CLS is not None and isinstance(exc, _GROUP_CLS):
        members = []
        for sub in list(getattr(exc, "exceptions", ()))[:10]:
            if id(sub) in seen:
                continue
            m = _one(sub, in_app_prefixes, config, seen)
            m["is_cause"] = False
            m["relation"] = "group"
            members.append(m)
        if members:
            payload["group"] = members
    return payload


def _next_in_chain(exc: BaseException) -> Tuple[Optional[BaseException], str]:
    cause = getattr(exc, "__cause__", None)
    if cause is not None:
        return cause, "cause"
    if getattr(exc, "__suppress_context__", False):
        return None, ""
    ctx = getattr(exc, "__context__", None)
    if ctx is not None:
        return ctx, "context"
    return None, ""


def _one(exc: BaseException, in_app_prefixes: Optional[List[str]], config: Any, seen: set) -> Dict[str, Any]:
    seen.add(id(exc))
    module = type(exc).__module__ or ""
    out: Dict[str, Any] = {
        "type": type(exc).__name__,
        "module": "" if module == "builtins" else module,
        "message": safe_str(exc, 8192),
        "frames": frames_from_traceback(getattr(exc, "__traceback__", None), in_app_prefixes=in_app_prefixes, config=config),
    }
    notes = getattr(exc, "__notes__", None)
    if notes:
        out["notes"] = [safe_str(n, 1000) for n in list(notes)[:10]]
    return out


def frames_from_traceback(tb: Any, *, in_app_prefixes: Optional[List[str]] = None, config: Any = None) -> List[Dict[str, Any]]:
    if tb is None:
        return []
    include = tuple(in_app_prefixes or ()) + tuple(_opt(config, "in_app_include") or ())
    exclude = tuple(_opt(config, "in_app_exclude") or ())
    with_locals = bool(_opt(config, "with_locals"))
    locals_scope = str(_opt(config, "locals_scope") or "in_app")
    context_lines = int(_opt(config, "context_lines"))
    max_frames = int(_opt(config, "max_frames"))

    colnos: List[Optional[int]] = []
    try:
        colnos = [getattr(fs, "colno", None) for fs in traceback.extract_tb(tb)]
    except Exception:
        colnos = []

    frames: List[Dict[str, Any]] = []
    idx = 0
    try:
        walker = traceback.walk_tb(tb)
    except Exception:
        return []
    for frame, lineno in walker:
        code = frame.f_code
        filename = code.co_filename or ""
        module = ""
        try:
            module = str(frame.f_globals.get("__name__") or "")
        except Exception:
            module = ""
        function = getattr(code, "co_qualname", None) or code.co_name or ""
        in_app = is_in_app(filename, module, include=include, exclude=exclude)
        context, pre, post = _source_context(filename, int(lineno or 0), context_lines, frame.f_globals)
        fr: Dict[str, Any] = {
            "filename": filename,
            "abs_path": _abs_path(filename),
            "module": module,
            "function": function,
            "lineno": int(lineno or 0),
            "context": context,
            "in_app": bool(in_app),
        }
        if idx < len(colnos) and colnos[idx] is not None:
            fr["colno"] = int(colnos[idx]) + 1
        if pre:
            fr["pre_context"] = pre
        if post:
            fr["post_context"] = post
        if with_locals and locals_scope != "none" and (locals_scope == "all" or in_app):
            v = safe_locals(frame.f_locals, max_items=int(_opt(config, "max_locals")), max_len=int(_opt(config, "max_local_repr")))
            if v:
                fr["vars"] = v
        frames.append(fr)
        idx += 1
    if len(frames) > max_frames > 0:
        head = max_frames // 2
        tail = max_frames - head
        frames = frames[:head] + frames[-tail:]
    return frames


def _abs_path(filename: str) -> str:
    if not filename or filename.startswith("<"):
        return filename
    try:
        return os.path.abspath(filename)
    except Exception:
        return filename


def _source_context(filename: str, lineno: int, n: int, module_globals: Any = None) -> Tuple[str, List[str], List[str]]:
    if not filename or lineno <= 0 or filename.startswith("<"):
        return "", [], []
    try:
        line = linecache.getline(filename, lineno, module_globals)
    except Exception:
        return "", [], []
    if not line:
        return "", [], []
    pre: List[str] = []
    post: List[str] = []
    if n > 0:
        for i in range(max(1, lineno - n), lineno):
            try:
                pre.append(linecache.getline(filename, i, module_globals).rstrip("\n"))
            except Exception:
                pre.append("")
        for i in range(lineno + 1, lineno + 1 + n):
            try:
                s = linecache.getline(filename, i, module_globals)
            except Exception:
                s = ""
            if not s:
                break
            post.append(s.rstrip("\n"))
    return line.strip(), pre, post


def is_in_app_file(filename: str, *, in_app_prefixes: Optional[List[str]] = None) -> bool:
    """0.1-compatible path heuristic."""
    if not filename:
        return False
    fn = filename.replace("\\", "/")
    if fn.startswith("<"):
        return False
    if in_app_prefixes:
        for p in in_app_prefixes:
            if p and fn.startswith(str(p).replace("\\", "/")):
                return True
    stdlib = (getattr(sys, "base_prefix", None) or sys.prefix).replace("\\", "/")
    if fn.startswith(stdlib + "/lib/"):
        return False
    lowered = fn.lower()
    for marker in ("/site-packages/", "/dist-packages/", "/.venv/", "/venv/", "/lib/python3", "importlib/_bootstrap"):
        if marker in lowered:
            return False
    return True


def is_in_app(filename: str, module: str, *, include: Sequence[str] = (), exclude: Sequence[str] = ()) -> bool:
    """include/exclude take module or path prefixes; include wins, then exclude, then heuristic."""
    fn = (filename or "").replace("\\", "/")
    for p in include:
        p = str(p)
        if p and (module == p or module.startswith(p + ".") or fn.startswith(p.replace("\\", "/"))):
            return True
    for p in exclude:
        p = str(p)
        if p and (module == p or module.startswith(p + ".") or fn.startswith(p.replace("\\", "/"))):
            return False
    return is_in_app_file(filename)


class _Repr(reprlib.Repr):
    def __init__(self, max_len: int) -> None:
        super().__init__()
        self.maxstring = max_len
        self.maxother = max_len
        self.maxlist = self.maxtuple = self.maxset = self.maxdict = 12
        self.maxlevel = 3


def safe_repr(value: Any, max_len: int = 256) -> str:
    try:
        s = _Repr(max_len).repr(value)
    except Exception:
        try:
            s = f"<{type(value).__name__} (unrepresentable)>"
        except Exception:
            s = "<unrepresentable>"
    if len(s) > max_len:
        s = s[: max_len - 1] + "…"
    return s


def safe_str(v: Any, max_len: int) -> str:
    try:
        s = str(v)
    except Exception:
        s = f"<{type(v).__name__}>"
    if len(s) > max_len:
        s = s[: max_len - 1] + "…"
    return s


def safe_locals(f_locals: Any, *, max_items: int = 20, max_len: int = 256) -> Dict[str, str]:
    out: Dict[str, str] = {}
    try:
        items = list(f_locals.items())
    except Exception:
        return out
    for name, value in items:
        name = str(name)
        if name.startswith("__") or name == "self" and False:
            continue
        if len(out) >= max_items:
            out["…"] = f"{len(items) - max_items} more"
            break
        if _SECRET_NAME.search(name):
            out[name] = "[REDACTED]"
            continue
        out[name] = safe_repr(value, max_len)
    return out


# --- dedupe: remember which exceptions were already reported --------------------------------

_ATTR = "__pyloggle_event_id__"
_fallback: Dict[int, Tuple[str, float]] = {}
_fallback_lock = threading.Lock()


def mark_captured(exc: BaseException, event_id: str) -> None:
    """Remember that exc was reported as event_id (so later log lines carrying it are
    duplicates) and stamp the active OpenTelemetry span, if any, with the event id."""
    try:
        from .otel import annotate_captured

        annotate_captured(event_id)
    except Exception:
        pass
    try:
        setattr(exc, _ATTR, event_id)
        return
    except Exception:
        pass
    now = time.time()
    with _fallback_lock:
        if len(_fallback) > 1000:
            for k, (_, t) in list(_fallback.items()):
                if now - t > 60:
                    _fallback.pop(k, None)
        _fallback[id(exc)] = (event_id, now)


def captured_event_id(exc: Optional[BaseException]) -> Optional[str]:
    if exc is None:
        return None
    v = getattr(exc, _ATTR, None)
    if v:
        return str(v)
    with _fallback_lock:
        ent = _fallback.get(id(exc))
    if ent and time.time() - ent[1] < 60:
        return ent[0]
    return None


def current_thread_name() -> str:
    try:
        return threading.current_thread().name
    except Exception:
        return ""


__all__ = [
    "exception_to_payload",
    "frames_from_traceback",
    "is_in_app_file",
    "is_in_app",
    "safe_repr",
    "safe_locals",
    "mark_captured",
    "captured_event_id",
]
