-- 长期公开资产链接（文章封面、头像、商品图、视频封面）。
--
-- 与签名 URL 的区别：这里的 token 是**有状态的能力凭证**——随机不可猜、可以
-- 永久有效（expires_at NULL）、可以单独撤销（revoked_at）。签名 URL 无状态、
-- 有硬上限（SigV4 7 天）、且无法撤销，只适合私有文件的临时下载。
--
-- token 直接入库（不做哈希）：它保护的是「公开可读」的资产，泄露 token 等于
-- 泄露一条本来就要对外发布的链接；它不承担机密性。要保密的内容走签名 URL。
CREATE TABLE IF NOT EXISTS file_shares (
    id         BIGSERIAL PRIMARY KEY,
    file_id    BIGINT      NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    -- crypto/rand 生成的 base64url token（默认 32 字节 → 43 字符）
    token      VARCHAR(64) NOT NULL,
    -- 用途隔离：同一张图可以同时有 article-cover（永久）与 og-image（30 天）
    purpose    VARCHAR(32) NOT NULL DEFAULT 'asset',
    -- NULL = 长期有效
    expires_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_by BIGINT      NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 同一 token 只能有一个有效行；撤销后 token 可以复用（查询仍按 revoked_at IS NULL）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_file_shares_token
    ON file_shares (token)
    WHERE revoked_at IS NULL;

-- 幂等创建：同一 (file_id, purpose) 只保留一个有效 share。
CREATE UNIQUE INDEX IF NOT EXISTS idx_file_shares_file_purpose
    ON file_shares (file_id, purpose)
    WHERE revoked_at IS NULL;

-- 撤销/删除级联按 file_id 查。
CREATE INDEX IF NOT EXISTS idx_file_shares_file
    ON file_shares (file_id);
