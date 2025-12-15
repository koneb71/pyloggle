-- Alerting schema extensions
USE pyloggle;

-- Add optional route/method filtering + cooldown for rules (MVP).
ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS http_route String AFTER env;

ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS http_method LowCardinality(String) AFTER http_route;

ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS cooldown_seconds UInt32 AFTER time_window_seconds;

-- Track alert firings to enforce cooldown across restarts and provide a basic audit trail.
CREATE TABLE IF NOT EXISTS alert_firings (
    id String,
    rule_id String,
    project_id String,
    service LowCardinality(String),
    env LowCardinality(String),
    condition_type LowCardinality(String),

    fired_at DateTime64(3, 'UTC'),
    window_start DateTime64(3, 'UTC'),
    window_end DateTime64(3, 'UTC'),

    -- A short stable key to reduce accidental duplicates (e.g. rule+window).
    dedupe_key String,

    -- JSON payload for debugging / replay.
    payload String,

    INDEX idx_rule (rule_id) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_project (project_id) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_fired_at (fired_at) TYPE minmax GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(fired_at)
ORDER BY (project_id, rule_id, fired_at)
TTL toDate(fired_at) + INTERVAL 180 DAY
SETTINGS index_granularity = 8192;


