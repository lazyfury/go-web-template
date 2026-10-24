-- 消息/通知：messages 是内容快照，message_recipients 是站内信收件箱，
-- message_deliveries 是邮件/短信/Webhook 的投递 outbox，message_templates 是模板。
CREATE TABLE IF NOT EXISTS messages (
    id           BIGSERIAL PRIMARY KEY,
    title        VARCHAR(128) NOT NULL,
    body         TEXT         NOT NULL,
    type         VARCHAR(32)  NOT NULL DEFAULT 'system',
    level        VARCHAR(16)  NOT NULL DEFAULT 'info',
    link         VARCHAR(512) NOT NULL DEFAULT '',
    audience     VARCHAR(16)  NOT NULL DEFAULT 'users',
    template_key VARCHAR(64)  NOT NULL DEFAULT '',
    created_by   BIGINT       REFERENCES users(id) ON DELETE SET NULL,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_created_by ON messages (created_by);
CREATE INDEX IF NOT EXISTS idx_messages_expires_at ON messages (expires_at);
CREATE INDEX IF NOT EXISTS idx_messages_deleted_at ON messages (deleted_at);

CREATE TABLE IF NOT EXISTS message_recipients (
    id         BIGSERIAL PRIMARY KEY,
    message_id BIGINT      NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    read_at    TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (message_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_message_recipients_inbox
    ON message_recipients (user_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_message_recipients_message
    ON message_recipients (message_id);
CREATE INDEX IF NOT EXISTS idx_message_recipients_deleted_at
    ON message_recipients (deleted_at);

CREATE TABLE IF NOT EXISTS message_deliveries (
    id                  BIGSERIAL PRIMARY KEY,
    message_id          BIGINT       NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    recipient_id        BIGINT       REFERENCES message_recipients(id) ON DELETE SET NULL,
    channel             VARCHAR(16)  NOT NULL,
    target              VARCHAR(512) NOT NULL,
    payload             TEXT         NOT NULL,
    status              SMALLINT     NOT NULL DEFAULT 0,
    attempts            INTEGER      NOT NULL DEFAULT 0,
    max_attempts        INTEGER      NOT NULL DEFAULT 5,
    next_attempt_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    locked_until        TIMESTAMPTZ,
    provider_message_id VARCHAR(128) NOT NULL DEFAULT '',
    last_error          TEXT         NOT NULL DEFAULT '',
    dedupe_key          VARCHAR(128) NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_message_deliveries_claim
    ON message_deliveries (status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_message_deliveries_message
    ON message_deliveries (message_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_message_deliveries_dedupe
    ON message_deliveries (dedupe_key) WHERE dedupe_key <> '';

CREATE TABLE IF NOT EXISTS message_templates (
    id           BIGSERIAL PRIMARY KEY,
    template_key VARCHAR(64)  NOT NULL,
    channel      VARCHAR(16)  NOT NULL,
    locale       VARCHAR(16)  NOT NULL DEFAULT 'zh-CN',
    subject      VARCHAR(255) NOT NULL DEFAULT '',
    body         TEXT         NOT NULL,
    variables    TEXT         NOT NULL DEFAULT '[]',
    status       SMALLINT     NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_message_templates_key_unique
    ON message_templates (template_key, channel, locale) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_message_templates_deleted_at ON message_templates (deleted_at);
