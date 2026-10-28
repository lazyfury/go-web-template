-- 文件管理：元数据表 + 跨模块引用表。
--
-- files 只存「对象存在哪里、叫什么、多大、谁能看」，字节在 blobx 驱动背后
-- （local 磁盘 / S3 兼容的 MinIO、阿里云 OSS、R2）。storage 列记录写入时的
-- 驱动名，切换驱动后旧行仍能定位到自己的后端。
--
-- 软删除 + status 两阶段删除：DELETE 先把行置 status=2（deleting），
-- 由 housekeeping worker 删掉 blob 后再硬删行。顺序反过来会留下永远无法
-- 定位的孤儿对象。
CREATE TABLE IF NOT EXISTS files (
    id            BIGSERIAL PRIMARY KEY,
    storage       VARCHAR(16)  NOT NULL DEFAULT 'local',
    bucket        VARCHAR(128) NOT NULL DEFAULT '',
    -- 存储 key（file/YYYYMM/<uuid><ext>），由服务端生成，从不使用用户文件名
    object_key    VARCHAR(512) NOT NULL,
    -- 清洗后的原始文件名，仅用于展示与 Content-Disposition
    original_name VARCHAR(255) NOT NULL,
    ext           VARCHAR(32)  NOT NULL DEFAULT '',
    mime_type     VARCHAR(128) NOT NULL DEFAULT '',
    size_bytes    BIGINT       NOT NULL,
    -- 内容哈希：流式上传时计算，给「秒传/去重」和取证留的锚点
    sha256        CHAR(64)     NOT NULL DEFAULT '',
    -- 业务类型：avatar / attachment / export ...（只用于分组与配额，不参与鉴权）
    biz_type      VARCHAR(32)  NOT NULL DEFAULT '',
    -- 0=private（默认，只有所有者/管理员可读）1=public
    visibility    SMALLINT     NOT NULL DEFAULT 0,
    -- 0=pending 1=ready 2=deleting
    status        SMALLINT     NOT NULL DEFAULT 1,
    owner_id      BIGINT       NULL REFERENCES users (id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ  NULL
);

-- 同一个后端上的同一个 key 只能有一行；软删后允许同 key 复用（镜像既有约定）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_files_object_key
    ON files (storage, bucket, object_key)
    WHERE deleted_at IS NULL;

-- 「我的文件」列表：按 owner 过滤 + 时间倒序。
CREATE INDEX IF NOT EXISTS idx_files_owner
    ON files (owner_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- 清理任务按 (status, created_at) 捞 pending/deleting 行。
CREATE INDEX IF NOT EXISTS idx_files_purge
    ON files (status, created_at)
    WHERE deleted_at IS NULL;

-- 去重/秒传候选查询（sha256 + 大小）。
CREATE INDEX IF NOT EXISTS idx_files_sha256
    ON files (sha256, size_bytes)
    WHERE deleted_at IS NULL;

-- file_links 是其他模块引用文件的唯一通道：owner_type 由业务方定义
-- （user.avatar / message.attachment / post.cover），owner_id 是业务行 id。
-- 不做跨域外键：业务表软删顺序不可控，硬外键只会把删除变成竞态。
CREATE TABLE IF NOT EXISTS file_links (
    id         BIGSERIAL PRIMARY KEY,
    file_id    BIGINT      NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    owner_type VARCHAR(32) NOT NULL,
    owner_id   BIGINT      NOT NULL,
    -- 单槽位场景（如头像 avatar）；空字符串表示多附件集合
    slot       VARCHAR(32) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ NULL
);

-- 同一资源的同一槽位不能重复挂同一个文件（挂载接口幂等）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_file_links_unique
    ON file_links (owner_type, owner_id, slot, file_id)
    WHERE deleted_at IS NULL;

-- 按资源列附件：「这个订单有哪些文件」。
CREATE INDEX IF NOT EXISTS idx_file_links_owner
    ON file_links (owner_type, owner_id)
    WHERE deleted_at IS NULL;

-- 按文件反查引用：判断孤儿、删除前检查是否仍被引用。
CREATE INDEX IF NOT EXISTS idx_file_links_file
    ON file_links (file_id)
    WHERE deleted_at IS NULL;
