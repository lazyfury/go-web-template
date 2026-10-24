-- 000004_add_refresh_session_start.up.sql
-- 会话绝对上限 + 吊销原因：
--   session_started_at 跨轮换继承，限制一次登录的最长寿命；
--   revoked_reason 区分「轮换吊销」（允许 reuse interval 容错）与
--   「登出/改密/重用检测/会话到期」（不允许容错，避免登出后被复活）。
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS session_started_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS revoked_reason VARCHAR(32);
