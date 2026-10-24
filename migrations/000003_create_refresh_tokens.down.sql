-- 000003_create_refresh_tokens.down.sql
DROP INDEX IF EXISTS idx_refresh_tokens_expires_at;
DROP INDEX IF EXISTS idx_refresh_tokens_user_active;

DROP TABLE IF EXISTS refresh_tokens;
