-- 用户手机号：消息模块的短信渠道需要联系方式。
-- 空字符串表示未设置（避免 NULL 在 Go DTO / 前端表单里引入额外分支）。
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone VARCHAR(32) NOT NULL DEFAULT '';
