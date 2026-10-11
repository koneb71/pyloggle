-- Alerting v2: log-query monitors, Slack targets, links and counts on firings.
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS query String AFTER http_method;

ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS comparison LowCardinality(String) AFTER threshold;

ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS slack_webhook_urls Array(String) AFTER telegram_chat_ids;

ALTER TABLE alert_firings ADD COLUMN IF NOT EXISTS count UInt64 AFTER window_end;

ALTER TABLE alert_firings ADD COLUMN IF NOT EXISTS url String AFTER count;
