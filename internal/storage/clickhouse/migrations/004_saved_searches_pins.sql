-- Saved searches schema extensions (pinned + soft-delete)
USE pyloggle;

ALTER TABLE saved_searches
    ADD COLUMN IF NOT EXISTS is_pinned UInt8 AFTER is_public;

ALTER TABLE saved_searches
    ADD COLUMN IF NOT EXISTS is_deleted UInt8 AFTER is_pinned;


