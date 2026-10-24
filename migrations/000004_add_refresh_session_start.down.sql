-- 000004_add_refresh_session_start.down.sql
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS revoked_reason;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS session_started_at;
