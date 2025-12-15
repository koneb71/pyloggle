-- Alerting: Telegram notifier support
USE pyloggle;

ALTER TABLE alert_rules
    ADD COLUMN IF NOT EXISTS telegram_chat_ids Array(String) AFTER webhook_urls;


