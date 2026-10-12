-- What each alert rule has already sent, so a problem alerts a rule only once (or once a day
-- for jumps), and count rules alert once per incident. subject is "issue:<id>",
-- "came_back:<activity id>", "jump:<id>", "incident" (open = 1 while it lasts) or
-- "recap:<date>".
CREATE TABLE IF NOT EXISTS alert_notified (
    project_id String,
    rule_id String,
    subject String,
    open UInt8,
    notified_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(notified_at)
ORDER BY (project_id, rule_id, subject)
TTL toDate(notified_at) + INTERVAL 400 DAY
SETTINGS index_granularity = 8192;

-- Recap rules: the local hour to send at and the time zone that defines "yesterday".
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS send_hour UInt8 DEFAULT 8;

ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS timezone String DEFAULT 'UTC';
