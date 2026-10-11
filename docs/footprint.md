# Footprint

Pyloggle is two processes: `ingestd` (one static Go binary that ingests, serves the UI/API,
runs alerting and exposes `/metrics`) and ClickHouse. This page records what they cost, how it
was measured and which knobs to turn. Re-measure with `make footprint` (compose) or the local
mode described at the top of `scripts/footprint.sh`; the System page and `GET /metrics` show
the same table sizes live.

## 0.3: spans and metrics on (ClickHouse 24.3.18, same sandbox, local mode)

0.3 stores OTLP spans (7 days) and metric points (30 days) by default and keeps request stats
per minute. Measured on a fresh start of both processes, then 20 000 requests through FastAPI's
native telemetry (`examples/otel_app.py`: four spans per request, HTTP metrics every 5 s, one
crash in 25, 80 000 spans in 25 s), then the same 100 000-line log burst as below. Local mode
uses the dev ClickHouse config (mark cache 256 MB instead of the compose file's 128 MB).

| process | fresh idle | after the OTel load | peak during the log burst | after both |
|---|---|---|---|---|
| clickhouse | 412 MB (109 MB anonymous) | 446 MB (136 MB anonymous) | 630 MB | 508–570 MB (258 MB anonymous) |
| ingestd | 15 MB (3 MB anonymous) | 96 MB (85 MB anonymous) | 124 MB | 124 MB (112 MB anonymous) |

ingestd's resting memory after a burst is its Go heap goal (about twice the live heap, which
holds the four batch queues once they have been filled, the issue-state and dedupe caches and
the collector snapshot); the compose file's `GOMEMLIMIT=200MiB` bounds it, and it is not
released to the OS while the process stays idle. The ClickHouse figures include the new
materialized views (usage, request stats, releases) that run on every insert.

| table | rows | on disk | uncompressed | ratio | bytes/row |
|---|---|---|---|---|---|
| spans | 160 546 | 5.9 MiB | 25.9 MiB | 4.4 | 39 |
| request_stats_minutely | 13 417 | 1.6 MiB | 3.2 MiB | 2.0 | 124 |
| metrics_points | 1 042 | 14 KiB | 292 KiB | 20.9 | 14 |
| project_usage_daily | 24 | 1.7 KiB | 1 KiB | | |

A traced request from FastAPI's native telemetry costs four spans, about 160 bytes on disk,
kept for seven days; a cumulative histogram series costs one row per export interval (14 bytes
each with the default 15 buckets). Rule of thumb with everything on:

```
disk ≈ lines/day × ~100 B × 30 days + traced requests/day × ~160 B × 7 days + metric series × 1 440 × 14 B × 30 days
```

Log rows and error occurrences are unchanged from 0.2 (124 and 136 bytes per row in this run).
The ingest rate during the burst (5 300 lines/s paced, every line stored, queues never
saturating) is unchanged too.

## 0.2 measurement (ClickHouse 24.3.18, 4 vCPU sandbox, tuned config, 100k-line burst)

The burst is 100 000 synthetic log lines (four services, 80 % info / 10 % debug / 7 % warn /
3 % error, half of the errors carrying a two-frame exception) sent as 5 000-line NDJSON requests
paced at 6 000 lines/s. Debug lines are sampled at 10 % by default, so 90 977 lines were stored.

| process | idle RSS | peak during burst | after burst | of which anonymous (after) |
|---|---|---|---|---|
| clickhouse | 387 MB | 509 MB | 477 MB | 137 MB |
| ingestd | 15 MB | 71 MB | 63 MB | 52 MB |

RSS overstates ClickHouse: its executable is ~500 MB and every page of it that was touched
counts as resident, file-backed and shared with the page cache. The anonymous part is what the
server allocated for itself (caches, buffers, thread stacks). Under Docker `docker stats`
reports the cgroup's usage, which likewise includes those file pages.

Ingest throughput while paced: 5 200 lines/s sustained with every accepted line stored, queue
never saturating, batches of 500 rows flushed every second.

| table | rows | on disk | uncompressed | ratio | bytes/row |
|---|---|---|---|---|---|
| logs_raw | 124 505 | 15.0 MiB | 31.3 MiB | 2.1 | 126 |
| error_occurrences | 6 000 | 856 KiB | 4.5 MiB | 5.4 | 146 |
| issues_agg | 251 | 31 KiB | 68 KiB | 2.2 | 127 |
| issue_counts_hourly | 479 | 18 KiB | 48 KiB | 2.6 | 39 |
| log_counts_minutely | 2 913 | 15 KiB | 103 KiB | 7.0 | 5 |

The synthetic lines carry random 128-bit request and trace ids and random numbers in every
message, which is close to the worst case for compression. Real application logs repeat far
more and typically land at 40–80 bytes per row. Rule of thumb for capacity planning:

```
disk ≈ lines/day × ~100 B × 30 days          (e.g. 5 M lines/day ≈ 15 GB for a month)
```

System tables are capped: only `system.query_log` is kept, with a 3-day TTL (95 KiB after the
run). The stock configuration keeps ten more log tables that grow without bound.

## What keeps it small

**Daily partitions + TTL that drops whole parts.** `logs_raw` and `error_occurrences` are
partitioned by day and carry `TTL toDate(ts) + INTERVAL 30 DAY` with `ttl_only_drop_parts = 1`.
Expiry is a directory removal once a day, not a background merge that rewrites data. The
retention window is a runtime setting (`PYLOGGLE_RETENTION_LOGS_DAYS`,
`PYLOGGLE_RETENTION_ERRORS_DAYS`, `PYLOGGLE_RETENTION_STATS_DAYS`), applied with `MODIFY TTL`
at startup only when the value changed.

**Column codecs and low-cardinality columns.** Timestamps use `Delta + ZSTD`, messages, URLs and
JSON blobs `ZSTD(3)`, and service / env / level / logger / host / release are `LowCardinality`.
Bloom-filter indexes on `trace_id`, `request_id`, `event_id` and attribute keys plus a token
index on `message` keep the explorer's lookups off full scans.

**Pre-aggregated tables for the UI.** Issue counts, affected users, first/last seen and
per-level volume are maintained by materialized views (`issues_agg`, `issue_counts_hourly`,
`log_counts_minutely`), so list pages and dashboards never scan `logs_raw`.

**ClickHouse server overrides** (`deploy/clickhouse/config.d/pyloggle.xml`, mounted by compose):

| setting | pyloggle | stock | why |
|---|---|---|---|
| `max_server_memory_usage_to_ram_ratio` | 0.75 | 0.9 | cap relative to the container's `mem_limit` (1.5 GB → ~1.1 GB) |
| `mark_cache_size` | 128 MB | 5 GB | the working set is a few days of marks |
| `uncompressed_cache_size` | 0 | 0 | unchanged, kept explicit |
| `max_concurrent_queries` | 16 | 100 | the UI issues a handful per page |
| `max_thread_pool_size` | 500 | 10 000 | fewer idle threads |
| `background_pool_size` | 2 | 16 | merges are light with daily parts and no mutations |
| `background_schedule_pool_size` | 8 | 512 | no replication, no Kafka |
| `asynchronous_metrics_update_period_s` | 10 | 1 | metrics collectors wake up less |
| `merge_tree.ttl_only_drop_parts` | 1 | 0 | expiry drops parts instead of rewriting them |
| `mysql_port`, `postgresql_port`, `interserver_http_port` | removed | on | nothing uses them |
| `*_log` system tables | only `query_log`, 3-day TTL | 11 tables, no TTL | they grow forever otherwise |

**Per-query limits** (`deploy/clickhouse/users.d/pyloggle.xml`): `max_threads = 2`,
`max_memory_usage = 800 MB` with spill-to-disk for GROUP BY / ORDER BY above 300 MB,
`max_execution_time = 30 s`. A runaway explorer query cannot take the server down.

**Compose limits** (`deploy/docker-compose.yml`): ClickHouse `mem_limit 1.5 GB`, 2 CPUs;
ingestd `mem_limit 256 MB`, 1 CPU, `GOMEMLIMIT=200MiB` so Go's collector works harder before
the cgroup limit is reached.

**ingestd itself.** One static binary, stdlib HTTP server, batched inserts (500 rows or 5 s),
bounded in-memory queue (`INGEST_MAX_QUEUE_SIZE`, default 10 000) with per-stream fairness,
per-service rate limit (`INGEST_RATE_LIMIT_RPS`, default 2 000/s, burst 4 000) and sampling
(`INGEST_SAMPLE_INFO` 1.0, `INGEST_SAMPLE_DEBUG` 0.1). Every drop is counted in the ingest
response (`dropped: {rate_limit, sampled, queue_full}`) and on `/healthz?verbose=1`.

## Knobs to turn

| I want to… | change |
|---|---|
| keep logs longer / shorter | `PYLOGGLE_RETENTION_LOGS_DAYS` (and `_ERRORS_DAYS`); applied at next start |
| keep every debug line | `INGEST_SAMPLE_DEBUG=1.0` |
| accept a bigger burst per service | `INGEST_RATE_LIMIT_RPS` / `INGEST_RATE_LIMIT_BURST`, or raise `max_events_per_second` on the project |
| give ClickHouse more room | raise `mem_limit`; the ratio in config.d follows it. Raise `mark_cache_size` for > 10 M rows/day |
| run on a 1 GB box | `mem_limit: 1g` for ClickHouse and `max_memory_usage` 500 MB in users.d; keep retention at 14 days |
| cap one project's disk | `max_storage_gb` on the project (ingest refuses once exceeded) |
| see what a query cost | `SELECT query, memory_usage, read_rows FROM system.query_log WHERE type = 'QueryFinish' ORDER BY event_time DESC LIMIT 20` |

## Reproducing the numbers

```
make compose-up && make footprint          # Docker: uses the two container names
# or, against a locally running pair:
FOOTPRINT_MODE=local CH_PID=<clickhouse pid> INGESTD_PID=<ingestd pid> \
  INGEST_URL=http://127.0.0.1:8080 CLICKHOUSE_HTTP=http://127.0.0.1:8123 CH_DB=pyloggle_dev \
  ./scripts/footprint.sh
```

The script creates a throwaway project (when UI auth is disabled), sends the SDK golden
fixtures and the burst, waits until every accepted line is stored, then prints the RSS and
`system.parts` tables above. `FOOTPRINT_LINES`, `FOOTPRINT_BATCH` and `FOOTPRINT_RATE` scale the
burst.
