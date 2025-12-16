-- Persist end-user and server addresses on events (structured columns).
-- Safe to run multiple times.
USE pyloggle;

ALTER TABLE logs_raw
    ADD COLUMN IF NOT EXISTS http_remote_addr String AFTER request_id;

ALTER TABLE logs_raw
    ADD COLUMN IF NOT EXISTS http_server_addr String AFTER http_remote_addr;

ALTER TABLE error_occurrences
    ADD COLUMN IF NOT EXISTS http_server_addr String AFTER http_remote_addr;


