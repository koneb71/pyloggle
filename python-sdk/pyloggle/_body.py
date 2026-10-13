"""What a request body becomes in an event.

Every framework integration hands the body here, so a JSON post, a form, a file upload and a
binary blob are recorded the same way everywhere:

- JSON is parsed when the whole body was read, so the scrubber can blank secret fields by name.
- Forms (url-encoded or multipart) become a dict of their fields, for the same reason. Files are
  summarized as ``"[file report.pdf, application/pdf, 12.3 KB]"``; their bytes are never sent.
- Text stays text. Anything else (images, archives, protobuf) is summarized, never sent.

The value is scrubbed and then cut to ``max_request_body_bytes`` by the client
(``Client._fit_body``), in that order, so cutting can never expose a secret.
"""
from __future__ import annotations

import json
from typing import Any, Iterable, Optional, Tuple

# How much of a body may be read to make sense of it. A larger body is not pulled into memory
# just to be recorded: it is summarized, or recorded as the text read so far.
PARSE_CAP = 64 * 1024

FORM = "application/x-www-form-urlencoded"
MULTIPART = "multipart/form-data"

_TEXT_TYPES = {
    "application/xml",
    "application/graphql",
    "application/javascript",
    "application/x-ndjson",
    "application/ndjson",
    "application/x-yaml",
    "application/yaml",
}


def enabled(config: Any) -> bool:
    """Whether bodies are recorded at all (on unless ``emit_request_body=False``)."""
    if not bool(getattr(config, "emit_request_body", True)):
        return False
    try:
        return int(getattr(config, "max_request_body_bytes", 4096) or 0) > 0
    except Exception:
        return False


def read_cap(config: Any) -> int:
    """How many bytes an integration may read: enough to parse, and never less than the limit."""
    try:
        limit = int(getattr(config, "max_request_body_bytes", 4096) or 0)
    except Exception:
        limit = 0
    return max(PARSE_CAP, limit)


def media_type(content_type: Optional[str]) -> str:
    """``"application/json; charset=utf-8"`` -> ``"application/json"``."""
    return str(content_type or "").split(";", 1)[0].strip().lower()


def is_json(media: str) -> bool:
    return media == "application/json" or media.endswith("+json")


def is_text(media: str) -> bool:
    return media.startswith("text/") or media in _TEXT_TYPES or media.endswith("+xml")


def is_form(media: str) -> bool:
    return media in (FORM, MULTIPART)


def human_size(n: Optional[int]) -> str:
    if n is None or n < 0:
        return ""
    if n < 1024:
        return f"{n} B"
    size = float(n)
    for unit in ("KB", "MB", "GB"):
        size /= 1024.0
        if size < 1024 or unit == "GB":
            break
    return f"{size:.1f} {unit}"


def summary(what: str, size: Optional[int] = None) -> str:
    """``"[image/png body, 1.2 MB, not recorded]"``."""
    parts = [f"{what or 'binary'} body"]
    if size is not None:
        parts.append(human_size(size))
    parts.append("not recorded")
    return "[" + ", ".join(parts) + "]"


def file_note(filename: Optional[str], content_type: Optional[str], size: Optional[int]) -> str:
    """``"[file report.pdf, application/pdf, 12.3 KB]"``."""
    parts = ["file " + (filename or "(no name)")]
    ct = media_type(content_type)
    if ct:
        parts.append(ct)
    if size is not None and size >= 0:
        parts.append(human_size(size))
    return "[" + ", ".join(parts) + "]"


def _collect(pairs: Iterable[Tuple[str, Any]]) -> dict:
    """Form pairs to a dict, in their order; a name sent twice keeps every value in a list."""
    out: dict = {}
    for k, v in pairs:
        k = str(k)
        if k in out:
            prev = out[k]
            out[k] = prev + [v] if isinstance(prev, list) else [prev, v]
        else:
            out[k] = v
    return out


def _lists(multidict: Any) -> Iterable[Tuple[str, list]]:
    """(name, values) from a Werkzeug MultiDict, a Django QueryDict or a plain mapping."""
    lists = getattr(multidict, "lists", None)
    if callable(lists):
        return lists()
    return ((k, v if isinstance(v, list) else [v]) for k, v in dict(multidict).items())


def _upload_size(f: Any) -> Optional[int]:
    size = getattr(f, "size", None)  # Django UploadedFile
    if isinstance(size, int):
        return size
    length = getattr(f, "content_length", None)  # Werkzeug FileStorage, when the client sent it
    if isinstance(length, int) and length > 0:
        return length
    stream = getattr(f, "stream", None)
    try:
        pos = stream.tell()
        stream.seek(0, 2)
        end = stream.tell()
        stream.seek(pos)
        return int(end)
    except Exception:
        return None


def from_form(form: Any = None, files: Any = None) -> Optional[dict]:
    """A dict of a parsed form's fields, with each uploaded file summarized."""
    pairs: list = []
    try:
        for k, values in _lists(form or {}):
            pairs.extend((k, str(v)) for v in values)
    except Exception:
        pass
    try:
        for k, values in _lists(files or {}):
            for f in values:
                name = getattr(f, "filename", None) or getattr(f, "name", None)
                ctype = getattr(f, "mimetype", None) or getattr(f, "content_type", None)
                pairs.append((k, file_note(name, ctype, _upload_size(f))))
    except Exception:
        pass
    return _collect(pairs) if pairs else None


def _parse_multipart(raw: bytes, content_type: str) -> Optional[dict]:
    """Parse a complete multipart body with the standard library (no extra dependency)."""
    from email import policy
    from email.parser import BytesParser

    head = b"Content-Type: " + content_type.encode("latin-1", "replace") + b"\r\n\r\n"
    msg = BytesParser(policy=policy.HTTP).parsebytes(head + raw)
    if not msg.is_multipart():
        return None
    pairs: list = []
    for part in msg.iter_parts():
        name = part.get_param("name", header="content-disposition") or ""
        payload = part.get_payload(decode=True) or b""
        filename = part.get_filename()
        if filename is not None:
            pairs.append((name, file_note(filename, part.get_content_type(), len(payload))))
        else:
            pairs.append((name, payload.decode(part.get_content_charset() or "utf-8", "replace")))
    return _collect(pairs)


def _decode_text(raw: bytes, complete: bool) -> Optional[str]:
    """UTF-8 text, or None for bytes that are not text. A body cut mid-character still counts."""
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError as e:
        if not complete and e.start >= len(raw) - 3:
            try:
                return raw[: e.start].decode("utf-8")
            except UnicodeDecodeError:
                return None
        return None


def describe_body(
    raw: Optional[bytes],
    content_type: Optional[str],
    *,
    complete: bool = True,
    size: Optional[int] = None,
    form: Any = None,
    files: Any = None,
) -> Tuple[Any, bool]:
    """(what to record, whether it is only the start of the body). Callers guard it: a body
    that cannot be described is left out, never allowed to break the request.

    ``raw`` is what was read (at most ``read_cap`` bytes); ``complete`` says it is the whole
    body; ``size`` is the full length when known. Frameworks that already parsed a form pass
    ``form`` and ``files`` instead of the bytes.
    """
    media = media_type(content_type)
    if form is not None or files is not None:
        parsed = from_form(form, files)
        if parsed is not None:
            return parsed, False
    raw = bytes(raw or b"")
    if not raw:
        if size:  # too large to read, or already consumed by the app
            return summary(media or "request", size), False
        return None, False
    total = size if size is not None else (len(raw) if complete else None)

    if media == MULTIPART:
        if complete:
            try:
                parsed = _parse_multipart(raw, str(content_type))
                if parsed is not None:
                    return parsed, False
            except Exception:
                pass
        return summary(MULTIPART, total), False

    text = _decode_text(raw, complete)
    if text is None:
        return summary(media or "binary", total), False

    if media == FORM:
        import urllib.parse

        pairs = urllib.parse.parse_qsl(text, keep_blank_values=True)
        if pairs:
            return _collect(pairs), not complete
        return text, not complete

    if is_json(media) or (not media and text.lstrip()[:1] in ("{", "[")):
        if complete:
            try:
                return json.loads(text), False
            except ValueError:
                pass
        return text, not complete

    if is_text(media) or not media:
        return text, not complete
    return summary(media, total), False


def fit_body(body: Any, limit: int) -> Tuple[Any, bool]:
    """Cut an already-scrubbed body to ``limit`` bytes of UTF-8. Parsed bodies that fit keep
    their shape; a parsed body that does not fit becomes the start of its JSON text."""
    if body is None or limit <= 0:
        return None, False
    if isinstance(body, (dict, list)):
        text = json.dumps(body, ensure_ascii=False, separators=(",", ":"), default=str)
        if len(text.encode("utf-8")) <= limit:
            return body, False
    else:
        text = body if isinstance(body, str) else str(body)
    data = text.encode("utf-8")
    if len(data) <= limit:
        return text, False
    return data[:limit].decode("utf-8", "ignore"), True
