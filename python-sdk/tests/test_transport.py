import json
import logging
import time


def test_batches_are_ndjson_with_headers_and_gzip(fake_ingest, make_client):
    c = make_client(gzip=True, gzip_min_bytes=1)
    for i in range(3):
        c.emit({"level": "info", "message": f"m{i}", "attrs": {"n": i, "flag": True, "obj": {"a": 1}}})
    assert c.flush(5.0)
    evs = fake_ingest.wait_for(3)
    assert [e["message"] for e in evs] == ["m0", "m1", "m2"]
    req = fake_ingest.requests[0]
    assert req["headers"]["content-type"] == "application/x-ndjson"
    assert req["headers"]["x-api-key"] == "test-key"
    assert req["headers"]["user-agent"].startswith("pyloggle-python/")
    assert req["headers"]["content-encoding"] == "gzip"
    ev = evs[0]
    # enrichment
    assert ev["service"] == "svc" and ev["env"] == "test" and ev["project_id"] == "p"
    assert len(ev["event_id"]) == 32
    assert ev["ts"].endswith("Z") and "." in ev["ts"]
    assert ev["sdk"]["name"] == "pyloggle-python"
    assert ev["runtime"]["version"] and ev["runtime"]["pid"] > 0 and ev["runtime"]["thread"]
    assert ev["hostname"]
    # attrs are stringified on the wire (bool, int, nested)
    assert ev["attrs"] == {"n": "0", "flag": "true", "obj": '{"a":1}'}


def test_no_gzip_below_threshold(fake_ingest, make_client):
    c = make_client(gzip=True, gzip_min_bytes=10_000_000)
    c.emit({"level": "info", "message": "small"})
    c.flush(5.0)
    fake_ingest.wait_for(1)
    assert "content-encoding" not in fake_ingest.requests[0]["headers"]


def test_retry_on_5xx_and_429_then_success(fake_ingest, make_client):
    fake_ingest.statuses = [503, 429, 202]
    c = make_client(max_retries=3)
    c.emit({"level": "error", "message": "keep me"})
    assert c.flush(10.0)
    time.sleep(0.1)
    assert [r["status"] for r in fake_ingest.requests] == [503, 429, 202]
    assert fake_ingest.events[0]["message"] == "keep me"


def test_no_retry_on_4xx(fake_ingest, make_client, caplog):
    fake_ingest.statuses = [400]
    c = make_client(max_retries=3)
    with caplog.at_level(logging.WARNING, logger="pyloggle.sdk"):
        c.emit({"level": "info", "message": "bad"})
        c.flush(5.0)
    time.sleep(0.1)
    assert [r["status"] for r in fake_ingest.requests] == [400]
    assert any("dropping batch" in r.getMessage() for r in caplog.records)


def test_413_splits_batch(fake_ingest, make_client):
    fake_ingest.statuses = [413, 202, 202]
    c = make_client(gzip=False)
    for i in range(4):
        c.emit({"level": "info", "message": f"m{i}"})
    c.flush(5.0)
    evs = fake_ingest.wait_for(4)
    assert sorted(e["message"] for e in evs) == ["m0", "m1", "m2", "m3"]
    assert len(fake_ingest.requests) == 3


def test_flush_waits_for_in_flight_batch(fake_ingest, make_client):
    c = make_client(flush_interval_s=5.0)  # time-based flush would take 5s
    t0 = time.monotonic()
    c.emit({"level": "info", "message": "x"})
    assert c.flush(5.0)
    assert time.monotonic() - t0 < 2.0
    assert fake_ingest.events[0]["message"] == "x"


def test_close_drains_queue(fake_ingest, make_client):
    c = make_client(batch_size=2, flush_interval_s=10.0)
    for i in range(5):
        c.emit({"level": "info", "message": f"m{i}"})
    c.close()
    evs = fake_ingest.wait_for(5)
    assert len(evs) == 5


def test_oversize_event_is_degraded_not_dropped(fake_ingest, make_client):
    c = make_client(max_event_bytes=4000, gzip=False)
    big = "x" * 3000
    c.emit({
        "level": "error", "message": "big",
        "exception": {"type": "E", "message": "m", "frames": [{"filename": "f", "function": "g", "lineno": 1, "context": "", "vars": {"blob": big}}]},
        "breadcrumbs": [{"ts": "2025-01-01T00:00:00Z", "message": big}],
        "http": {"method": "POST", "route": "/x", "status": 500, "body": big},
    })
    c.flush(5.0)
    ev = fake_ingest.wait_for(1)[0]
    # shed in order: locals, then breadcrumbs; the body still fits at this limit
    assert "vars" not in ev["exception"]["frames"][0]
    assert "breadcrumbs" not in ev and "body" in ev["http"]
    assert len(json.dumps(ev)) <= 4000
    tight = make_client(max_event_bytes=1200, gzip=False)
    tight.emit({"level": "error", "message": "m" * 2000, "http": {"method": "POST", "route": "/x", "status": 500, "body": big}})
    tight.flush(5.0)
    ev2 = fake_ingest.wait_for(2)[1]
    assert "body" not in ev2["http"] and ev2["attrs"]["pyloggle.truncated"] == "true" and len(ev2["message"]) < 2000


def test_level_normalization_and_scrubbing(fake_ingest, make_client):
    c = make_client()
    c.emit({"level": "WARNING", "message": "token=abc", "attrs": {"api_key": "s3cret", "ok": "v"},
            "http": {"headers": {"Authorization": "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijklmnop", "User-Agent": "ua"}}})
    c.emit({"level": "CRITICAL", "message": "x"})
    c.flush(5.0)
    evs = fake_ingest.wait_for(2)
    assert evs[0]["level"] == "warn" and evs[1]["level"] == "fatal"
    assert evs[0]["attrs"]["api_key"] == "[REDACTED]" and evs[0]["attrs"]["ok"] == "v"
    assert evs[0]["http"]["headers"]["Authorization"] == "[REDACTED]"
    assert evs[0]["http"]["headers"]["User-Agent"] == "ua"
