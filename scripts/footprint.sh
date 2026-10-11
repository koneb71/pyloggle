#!/usr/bin/env bash
# Measures what the pyloggle stack costs: resident memory of ClickHouse and ingestd when idle and
# under a synthetic burst, on-disk bytes per table and the compression ratio.
#
#   make footprint                       # against the docker compose stack (starts it if needed)
#   FOOTPRINT_MODE=local \
#     INGEST_URL=http://127.0.0.1:8080 CLICKHOUSE_HTTP=http://127.0.0.1:8123 \
#     CH_PID=<pid> INGESTD_PID=<pid> ./scripts/footprint.sh
#
# Knobs: FOOTPRINT_LINES (default 100000 synthetic log lines), FOOTPRINT_BATCH (5000 lines per
# request), FOOTPRINT_RATE (lines per second the burst is paced at, default 6000: ingestd's
# per-service limiter allows 2000/s and the burst spreads over 4 services), PYLOGGLE_API_KEY
# (created on the fly when unset and UI auth is disabled).
set -euo pipefail
cd "$(dirname "$0")/.."

MODE=${FOOTPRINT_MODE:-docker}
INGEST_URL=${INGEST_URL:-http://127.0.0.1:8080}
LINES=${FOOTPRINT_LINES:-100000}
BATCH=${FOOTPRINT_BATCH:-5000}
RATE=${FOOTPRINT_RATE:-6000}
PROJECT_ID=${PYLOGGLE_PROJECT_ID:-}
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if [ "$MODE" = docker ]; then
  CLICKHOUSE_HTTP=${CLICKHOUSE_HTTP:-http://127.0.0.1:8123}
  CH_USER=${CH_USER:-pyloggle}; CH_PASS=${CH_PASS:-pyloggle}; CH_DB=${CH_DB:-pyloggle}
  if ! docker ps --format '{{.Names}}' | grep -q '^pyloggle-ingestd$'; then
    echo "starting compose stack…"; (cd deploy && docker compose up --build -d)
  fi
else
  CLICKHOUSE_HTTP=${CLICKHOUSE_HTTP:-http://127.0.0.1:8123}
  CH_USER=${CH_USER:-default}; CH_PASS=${CH_PASS:-}; CH_DB=${CH_DB:-pyloggle_dev}
  : "${CH_PID:?CH_PID required in local mode}"; : "${INGESTD_PID:?INGESTD_PID required in local mode}"
fi

chq() { curl -fsS "$CLICKHOUSE_HTTP/?user=$CH_USER&password=$CH_PASS&database=$CH_DB" --data-binary "$1"; }

rss_mb() { # prints "<clickhouse MB> <ingestd MB>"
  if [ "$MODE" = docker ]; then
    docker stats --no-stream --format '{{.Name}} {{.MemUsage}}' pyloggle-clickhouse pyloggle-ingestd \
      | awk '{split($2,a,"/"); v=a[1]; m=1; if (v ~ /GiB/) m=1024; else if (v ~ /KiB/) m=1/1024; gsub(/[A-Za-z]/,"",v); printf "%s %.0f\n", $1, v*m}' \
      | sort | awk '{printf "%s ", $2} END {print ""}'
  else
    echo "$(($(ps -o rss= -p "$CH_PID") / 1024)) $(($(ps -o rss= -p "$INGESTD_PID") / 1024))"
  fi
}

for i in $(seq 1 60); do curl -fsS "$INGEST_URL/healthz" >/dev/null 2>&1 && break; sleep 1; done
curl -fsS "$INGEST_URL/healthz" >/dev/null || { echo "ingestd not reachable at $INGEST_URL"; exit 1; }

API_KEY=${PYLOGGLE_API_KEY:-}
if [ -z "$API_KEY" ]; then
  # Create a throwaway project through the UI API (works when PYLOGGLE_UI_AUTH=disabled).
  id="footprint-$(date +%s)"
  resp=$(curl -fsS -X POST -H 'Content-Type: application/json' -H 'X-Pyloggle-Request: 1' \
    -d "{\"id\":\"$id\",\"name\":\"Footprint\",\"description\":\"synthetic load\"}" "$INGEST_URL/ui/api/projects")
  API_KEY=$(printf '%s' "$resp" | sed -n 's/.*"api_key":"\([^"]*\)".*/\1/p')
  [ -n "$API_KEY" ] || { echo "could not create a project: $resp"; exit 1; }
  PROJECT_ID=$id
  echo "created project $id"
fi
PROJECT_FILTER="1"
[ -n "$PROJECT_ID" ] && PROJECT_FILTER="project_id = '$PROJECT_ID'"

# Sums the accepted / dropped counters from an ingest response into $TMP/totals.
account() { python3 - "$1" "$TMP/totals" <<'PY'
import json, sys
r = json.load(open(sys.argv[1])); path = sys.argv[2]
try: t = json.load(open(path))
except Exception: t = {"accepted": 0, "rejected": 0, "dropped": {}}
t["accepted"] += r.get("accepted", 0); t["rejected"] += r.get("rejected", 0)
for k, v in (r.get("dropped") or {}).items(): t["dropped"][k] = t["dropped"].get(k, 0) + v
json.dump(t, open(path, "w"))
PY
}

post() { curl -fsS -o "$TMP/resp" -w '%{http_code}' -X POST -H "X-API-Key: $API_KEY" -H 'Content-Type: application/x-ndjson' --data-binary @"$1" "$INGEST_URL/v1/ingest"; }

echo "== idle"
sleep 2
read -r CH_IDLE IN_IDLE <<<"$(rss_mb)"
echo "clickhouse ${CH_IDLE} MB · ingestd ${IN_IDLE} MB"

echo "== golden fixtures"
for f in internal/model/testdata/*.ndjson; do printf '%s -> %s %s\n' "$f" "$(post "$f")" "$(cat "$TMP/resp")"; account "$TMP/resp"; done

echo "== synthetic burst: $LINES lines in batches of $BATCH"
python3 - "$LINES" "$BATCH" "$TMP" <<'PY'
import json, random, sys, time, os
n, batch, out = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
random.seed(7)
services = ["api", "worker", "scheduler", "billing"]
routes = ["/api/orders", "/api/orders/{id}", "/api/users/{id}", "/health", "/api/search"]
msgs = ["request completed", "cache miss for key {k}", "retrying upstream call ({k})", "user {k} logged in",
        "payment settled for order {k}", "slow query took {k}ms", "connection reset by peer"]
now = time.time()
i = 0
part = 0
while i < n:
    lines = []
    for _ in range(min(batch, n - i)):
        r = random.random()
        level = "info" if r < 0.8 else "debug" if r < 0.9 else "warn" if r < 0.97 else "error"
        ev = {
            "ts": time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(now - random.random() * 3600)) + ".%03dZ" % random.randint(0, 999),
            "service": random.choice(services), "env": "prod", "level": level, "logger": "app." + random.choice(["http", "db", "cache", "jobs"]),
            "message": random.choice(msgs).format(k=random.randint(1, 100000)),
            "request_id": "%032x" % random.getrandbits(128), "trace_id": "%032x" % random.getrandbits(128),
            "hostname": "node-%d" % random.randint(1, 6), "release": "2.%d.0" % random.randint(0, 3),
            "attrs": {"user_id": str(random.randint(1, 5000)), "region": random.choice(["eu-1", "us-1", "ap-1"]), "shard": str(random.randint(0, 15))},
            "http": {"method": random.choice(["GET", "POST"]), "route": random.choice(routes), "status": random.choice([200, 200, 200, 201, 404, 500]), "url": "https://example.test" + random.choice(routes)},
            "duration_ms": round(random.expovariate(1 / 40), 2),
        }
        if level == "error" and random.random() < 0.5:
            ev["exception"] = {"type": "TimeoutError", "module": "", "message": "upstream timed out after 30s",
                               "frames": [{"filename": "app/http/client.py", "module": "app.http.client", "function": "call", "lineno": 88, "context": "    resp = session.get(url, timeout=30)", "in_app": True},
                                          {"filename": "app/orders.py", "module": "app.orders", "function": "settle", "lineno": 142, "context": "    client.call(url)", "in_app": True}]}
        lines.append(json.dumps(ev, separators=(",", ":")))
    with open(os.path.join(out, "batch-%04d.ndjson" % part), "w") as f:
        f.write("\n".join(lines) + "\n")
    i += len(lines); part += 1
PY
read -r CH_MAX IN_MAX <<<"$(rss_mb)"
start=$(date +%s.%N)
pause=$(python3 -c "print($BATCH/$RATE)")
for f in "$TMP"/batch-*.ndjson; do
  code=$(post "$f")
  [ "$code" = 200 ] || [ "$code" = 202 ] || { echo "batch $f -> $code $(cat "$TMP/resp")"; exit 1; }
  account "$TMP/resp"
  read -r c i <<<"$(rss_mb)"; [ "$c" -gt "$CH_MAX" ] && CH_MAX=$c; [ "$i" -gt "$IN_MAX" ] && IN_MAX=$i
  sleep "$pause"
done
end=$(date +%s.%N)
secs=$(python3 -c "print(round($end-$start,1))")
ACCEPTED=$(python3 -c "import json; print(json.load(open('$TMP/totals'))['accepted'])")
echo "sent $LINES lines in ${secs}s ($(python3 -c "print(int($LINES/max($end-$start,0.001)))") lines/s); ingestd answered $(cat "$TMP/totals")"

echo "== waiting for the batches to land"
for i in $(seq 1 120); do
  rows=$(chq "SELECT count() FROM logs_raw WHERE $PROJECT_FILTER" 2>/dev/null || echo 0)
  [ "${rows:-0}" -ge "$ACCEPTED" ] && break
  read -r c j <<<"$(rss_mb)"; [ "$c" -gt "$CH_MAX" ] && CH_MAX=$c; [ "$j" -gt "$IN_MAX" ] && IN_MAX=$j
  sleep 1
done
echo "stored rows: $rows (accepted $ACCEPTED)"
sleep 3
read -r CH_AFTER IN_AFTER <<<"$(rss_mb)"
chq "OPTIMIZE TABLE logs_raw FINAL" >/dev/null 2>&1 || true

echo
echo "| process | idle RSS | peak during burst | after burst |"
echo "|---|---|---|---|"
echo "| clickhouse | ${CH_IDLE} MB | ${CH_MAX} MB | ${CH_AFTER} MB |"
echo "| ingestd | ${IN_IDLE} MB | ${IN_MAX} MB | ${IN_AFTER} MB |"
if [ "$MODE" = local ]; then
  # RSS counts the pages of the (very large) ClickHouse executable that were touched; the
  # anonymous part is what the process actually allocated.
  for p in "clickhouse:$CH_PID" "ingestd:$INGESTD_PID"; do
    awk -v n="${p%%:*}" '/^RssAnon/ {a=$2} /^RssFile/ {f=$2} END {printf "%s: anonymous %.0f MB · file-backed %.0f MB\n", n, a/1024, f/1024}' "/proc/${p##*:}/status"
  done
fi
echo
echo "| table | rows | on disk | uncompressed | ratio | bytes/row |"
echo "|---|---|---|---|---|---|"
chq "SELECT table, sum(rows) AS r, formatReadableSize(sum(bytes_on_disk)) AS d, formatReadableSize(sum(data_uncompressed_bytes)) AS u,
            round(sum(data_uncompressed_bytes)/greatest(sum(bytes_on_disk),1),1) AS ratio, round(sum(bytes_on_disk)/greatest(sum(rows),1),1) AS bpr
     FROM system.parts WHERE active AND database = currentDatabase() AND table IN ('logs_raw','error_occurrences','issues_agg','issue_counts_hourly','log_counts_minutely')
     GROUP BY table ORDER BY sum(bytes_on_disk) DESC FORMAT TSV" | awk -F'\t' '{printf "| %s | %s | %s | %s | %s | %s |\n", $1, $2, $3, $4, $5, $6}'
echo
echo "system tables on disk: $(chq "SELECT formatReadableSize(sum(bytes_on_disk)) FROM system.parts WHERE active AND database = 'system'")"
