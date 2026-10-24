-- 字典/配置：dict_groups 是一个 setting（分组），dict_items 是该 setting 的数组元素。
-- 应用模块按 group_key 读取整个数组；管理端维护 group 与 item。
CREATE TABLE IF NOT EXISTS dict_groups (
    id          BIGSERIAL PRIMARY KEY,
    group_key   VARCHAR(64)  NOT NULL,
    name        VARCHAR(64)  NOT NULL,
    description VARCHAR(255) NOT NULL DEFAULT '',
    status      SMALLINT     NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

-- 软删后允许复用同一个 group_key。
CREATE UNIQUE INDEX IF NOT EXISTS idx_dict_groups_key_unique
    ON dict_groups (group_key) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_dict_groups_deleted_at ON dict_groups (deleted_at);

CREATE TABLE IF NOT EXISTS dict_items (
    id          BIGSERIAL PRIMARY KEY,
    group_id    BIGINT       NOT NULL REFERENCES dict_groups(id) ON DELETE CASCADE,
    item_key    VARCHAR(64)  NOT NULL,
    label       VARCHAR(128) NOT NULL,
    value       VARCHAR(1024) NOT NULL DEFAULT '',
    value_type  VARCHAR(16)  NOT NULL DEFAULT 'string',
    sort_order  INTEGER      NOT NULL DEFAULT 0,
    status      SMALLINT     NOT NULL DEFAULT 1,
    remark      VARCHAR(255) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

-- 同一分组内 item_key 唯一；软删后可重建同 key。
CREATE UNIQUE INDEX IF NOT EXISTS idx_dict_items_group_key_unique
    ON dict_items (group_id, item_key) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_dict_items_group_sort
    ON dict_items (group_id, sort_order, id);
CREATE INDEX IF NOT EXISTS idx_dict_items_deleted_at ON dict_items (deleted_at);
