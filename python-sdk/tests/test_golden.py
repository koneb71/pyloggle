import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def test_committed_golden_has_every_shape():
    from tests import gen_golden

    assert gen_golden.check(gen_golden.OUT) == 0


def test_generator_produces_every_shape(tmp_path):
    env = dict(os.environ, PYTHONPATH=os.path.dirname(HERE))
    proc = subprocess.run([sys.executable, "-c", f"""
import sys; sys.path.insert(0, {HERE!r})
import gen_golden, json
lines = gen_golden.collect()
text = "\\n".join(lines)
missing = [k for k in gen_golden.REQUIRED if '"%s"' % k not in text]
assert not missing, missing
evs = [json.loads(l) for l in lines]
err = [e for e in evs if e.get("exception") and e["exception"].get("chain")][0]
assert err["exception"]["chain"][0]["type"] == "KeyError"
inner = err["exception"]["chain"][0]["frames"][-1]
assert inner["vars"]["api_key"] == "[REDACTED]"
assert err["http"]["headers"]["authorization"] == "[REDACTED]"
assert err["user"]["id_hash"] and "email" not in err["user"]
assert [b["message"] for b in err["breadcrumbs"]][-1] == "cache miss"
dup = [e for e in evs if e.get("skip_issue")][0]
assert dup["attrs"]["pyloggle.duplicate_of"] == err["event_id"]
print("ok")
"""], env=env, capture_output=True, text=True, timeout=60)
    assert proc.returncode == 0, proc.stderr + proc.stdout
