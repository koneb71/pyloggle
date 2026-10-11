# Schema

The ClickHouse schema is no longer applied through `docker-entrypoint-initdb.d`.
`ingestd` embeds its migrations (`internal/storage/clickhouse/migrations/*.sql` plus a few Go
steps) and applies the missing ones at startup, recording them in `schema_migrations`.

- Disable with `PYLOGGLE_AUTO_MIGRATE=false` (then run them yourself in order).
- Retention is configured with `PYLOGGLE_RETENTION_LOGS_DAYS`, `PYLOGGLE_RETENTION_ERRORS_DAYS`
  and `PYLOGGLE_RETENTION_STATS_DAYS`; changes are applied as `ALTER TABLE ... MODIFY TTL` at startup.
- The v2 upgrade rebuilds `logs_raw` and `error_occurrences` with daily partitions and copies only
  the retention window. Set `PYLOGGLE_MIGRATE_REBUILD=false` to add the new columns in place instead.
