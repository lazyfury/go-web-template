# 消息 / 通知模块设计（未实现）

> 状态：设计稿（Phase 0），**尚未写业务代码**。
> 目标：站内信 + 邮件 + 短信 + Webhook + 模板 + 保留期限；SQL 为唯一事实来源，Redis 只做可选加速。
> 关联规范：`AGENTS.md` 硬性规则 12、`context.md` ADR 14。

## 1. 结论

**不做通用 event 系统，也不只建一张消息表。**

采用 **SQL-first 通知子系统**：

```text
HTTP API ──► message.Service ──► messages / message_recipients
                                  message_deliveries（投递 outbox）
                                  message_templates
                                        │
                                  message.Worker（cmd/server 启动）
                                        │
                    ┌───────────────────┼───────────────────┐
                    ▼                   ▼                   ▼
                 MailSender          SMSSender          WebhookSender
```

- 站内信：`message_recipients` 就是收件箱与已读状态，不需要额外投递表。
- 邮件/短信/Webhook：写 `message_deliveries`（pending）后由 worker 异步发送、重试、记录结果。
- `message_deliveries` 本身充当 outbox；v1 不引入 `outbox_events`。
- 未来跨域事件变多时，再加 `outbox_events` + dispatcher；`message` 只是其中一个 subscriber。
- Redis 不是 v1 依赖。只有吞吐/实时/多实例需求出现时才引入。

## 2. 需求与边界

### 必须支持

- 管理员发送：指定用户 / 全员 / 按角色。
- 渠道：站内信、邮件、短信、Webhook。
- 模板：按 key + channel 维护，发送时渲染并快照。
- 站内信：列表、未读数、标记已读、全部已读、删除。
- 投递：失败重试、指数退避、最大次数、失败原因、幂等。
- 保留期限：过期消息/投递记录可清理。
- 权限：member 只能看自己的收件箱；admin 才能发送/管理模板。

### 非目标（v1）

- 不做通用事件总线。
- 不做 Redis 队列 / Redis Pub/Sub。
- 不做 SSE/WebSocket 实时推送（前端轮询未读数）。
- 不做用户自定义 webhook endpoint（先走服务端配置/白名单）。
- 不做多语言模板（预留 `locale` 字段）。

## 3. 数据模型

迁移文件：`migrations/000006_add_phone_to_users.*` + `migrations/000007_create_message_tables.*`。
`users.phone` 用 `VARCHAR(32) NOT NULL DEFAULT ''`（空字符串 = 未设置，避免 NULL 在 DTO/前端表单里的额外分支）。

### `messages`：消息内容

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `title` | varchar(128) NOT NULL | 标题 |
| `body` | text NOT NULL | 渲染后的正文快照 |
| `type` | varchar(32) NOT NULL DEFAULT 'system' | `system` / `announcement` / `alert` |
| `level` | varchar(16) NOT NULL DEFAULT 'info' | `info` / `success` / `warning` / `error` |
| `link` | varchar(512) NOT NULL DEFAULT '' | 站内跳转 |
| `audience` | varchar(16) NOT NULL DEFAULT 'users' | `users` / `all` / `role`，审计用 |
| `template_key` | varchar(64) NOT NULL DEFAULT '' | 来源模板（可空） |
| `created_by` | BIGINT NULL REFERENCES users(id) | 管理员；系统消息为空 |
| `expires_at` | timestamptz NULL | 保留期限；空 = 按全局策略 |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | 软删除 |

索引：`(created_at DESC)`、`(created_by)`、`(expires_at)`。

### `message_recipients`：站内信收件箱

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `message_id` | BIGINT NOT NULL FK→messages | |
| `user_id` | BIGINT NOT NULL FK→users | |
| `read_at` | timestamptz NULL | 已读时间 |
| `deleted_at` | timestamptz NULL | 用户删除自己的收件记录 |
| `created_at` | timestamptz NOT NULL DEFAULT now() | |

索引/约束：

```sql
UNIQUE (message_id, user_id)
INDEX (user_id, read_at, created_at DESC)  -- 收件箱/未读数
INDEX (message_id)
```

全员广播在发送事务内批量 fan-out。用户量很大时再改成 `audience=all` + 独立 read receipts；v1 保持简单。

### `message_deliveries`：外部渠道投递（outbox）

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `message_id` | BIGINT NOT NULL FK→messages | |
| `recipient_id` | BIGINT NULL FK→message_recipients | Webhook 等无具体收件人时为空 |
| `channel` | varchar(16) NOT NULL | `email` / `sms` / `webhook` |
| `target` | varchar(512) NOT NULL | 发送时快照：邮箱/手机号/URL |
| `payload` | text NOT NULL | 渲染后的 subject/body 等 JSON 文本 |
| `status` | smallint NOT NULL DEFAULT 0 | 0 pending / 1 sending / 2 sent / 3 failed / 4 dead / 5 canceled |
| `attempts` | integer NOT NULL DEFAULT 0 | 已尝试次数 |
| `max_attempts` | integer NOT NULL DEFAULT 5 | |
| `next_attempt_at` | timestamptz NOT NULL DEFAULT now() | 下次可领取时间 |
| `locked_until` | timestamptz NULL | 多实例租约 |
| `provider_message_id` | varchar(128) NOT NULL DEFAULT '' | 服务商返回 ID |
| `last_error` | text NOT NULL DEFAULT '' | 最近一次错误（脱敏） |
| `dedupe_key` | varchar(128) NOT NULL DEFAULT '' | 幂等键（可选） |
| `created_at` / `updated_at` | timestamptz | |

索引：

```sql
INDEX (status, next_attempt_at)          -- worker 领取
INDEX (message_id)
INDEX (dedupe_key)
```

领取任务用 **`locked_until` 租约 + 条件更新**，不依赖 PostgreSQL 的 `SKIP LOCKED`，保证 SQLite 测试可跑：

```sql
UPDATE message_deliveries
SET status = 1, locked_until = ?, attempts = attempts + 1
WHERE id = ? AND status = 0 AND next_attempt_at <= ? AND (locked_until IS NULL OR locked_until < ?)
```

### `message_templates`：模板

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `key` | varchar(64) NOT NULL | 如 `user.welcome` |
| `channel` | varchar(16) NOT NULL | `inbox` / `email` / `sms` / `webhook` |
| `locale` | varchar(16) NOT NULL DEFAULT 'zh-CN' | 预留 |
| `subject` | varchar(255) NOT NULL DEFAULT '' | 邮件/站内信标题模板 |
| `body` | text NOT NULL | 模板正文 |
| `variables` | text NOT NULL DEFAULT '[]' | JSON 数组：允许的变量名 |
| `status` | smallint NOT NULL DEFAULT 1 | 启用/停用 |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

约束：`UNIQUE (key, channel, locale) WHERE deleted_at IS NULL`。
`variables` 用 TEXT 存 JSON 数组，避免 JSONB（SQLite 测试/可移植 SQL）。

## 4. 进程与协程生命周期（硬规范）

### 规则

1. **根 goroutine 只在 `cmd/server` 启动/回收**。
2. `internal/app` 只构造 repo/service/worker，并通过 `App` 暴露 worker；**禁止在 `app.New` / `app.Run` 内 `go`**。
3. service/handler 只写 DB（消息 + 收件人 + 投递记录），不直接发邮件/短信/Webhook，也不起 goroutine。
4. worker 必须：
   - `Run(ctx context.Context) error`；
   - ticker/clock 可注入（测试不 sleep）；
   - ctx 取消后停止领取新任务，等待在途任务；
   - 失败返回 error，由 `errgroup` 取消 HTTP。
5. 禁止裸 `go func()` 无回收、禁止 `time.Sleep` 轮询、禁止在 handler 里 `go sendMail()`。
6. Worker 内部确需并发时，使用有界 pool，由 `Run` 统一 `Wait`；pool 大小来自配置。

### cmd/server 接线（Phase 3 落地）

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

application, err := app.New(cfg, logger, app.WithVersion(version))
if err != nil {
    logger.Fatal("init application", zap.Error(err))
}
defer application.Close()

g, gctx := errgroup.WithContext(ctx)

g.Go(func() error { return application.Run(gctx) }) // HTTP
if worker := application.MessageWorker(); worker != nil {
    g.Go(func() error { return worker.Run(gctx) })
}

if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
    logger.Fatal("application exited with error", zap.Error(err))
}
```

要点：

- `App.MessageWorker()` 返回已装配好的 worker（未启用时返回 nil），不启动。
- 任一组件失败 → `errgroup` 取消另一个 → `g.Wait()` → 才 `App.Close()`。
- 多实例时 worker 通过 `locked_until` 租约避免重复发送。

## 5. API 草案

### 用户侧（`messages:read`）

| 端点 | 方法 | 说明 |
|---|---|---|
| `/api/v1/messages/inbox` | GET | 我的收件箱：`page/page_size/unread_only` |
| `/api/v1/messages/unread-count` | GET | 未读数 |
| `/api/v1/messages/inbox/:id` | GET | 详情（不自动已读） |
| `/api/v1/messages/inbox/:id/read` | PATCH | 标记已读（幂等） |
| `/api/v1/messages/read-all` | POST | 全部已读 |
| `/api/v1/messages/inbox/:id` | DELETE | 删除自己的收件记录（软删） |

### 管理侧（`messages:send` / `messages:manage`）

| 端点 | 方法 | 说明 |
|---|---|---|
| `/api/v1/messages` | POST | 发送：收件人 + channels + 内容/模板 |
| `/api/v1/messages/sent` | GET | 已发送列表 + 投递统计 |
| `/api/v1/message-templates` | GET/POST | 模板列表/新建 |
| `/api/v1/message-templates/:id` | GET/PATCH/DELETE | 模板维护 |
| `/api/v1/message-deliveries` | GET | 投递记录（按状态/渠道过滤） |
| `/api/v1/message-deliveries/:id/retry` | POST | 手动重试 dead/failed |

发送请求草案：

```json
{
  "title": "系统维护通知",
  "body": "今晚 23:00 维护",
  "type": "announcement",
  "level": "warning",
  "link": "/dashboard",
  "channels": ["inbox", "email", "webhook"],
  "recipients": {
    "all": false,
    "user_ids": [2, 3],
    "roles": ["admin"]
  },
  "template_key": "system.maintenance",
  "data": { "window": "23:00" }
}
```

约束：

- `recipients` 三选一，不能同时为空；`all` 与 `user_ids/roles` 互斥。
- `template_key` 存在时，`title/body` 可空；渲染后写快照。
- Webhook target 只允许服务端配置/白名单，禁止管理端任意 URL（防 SSRF）。

## 6. 投递状态机与重试

```text
pending ──claim──► sending ──success──► sent
   ▲                  │
   │                  └──failure──► failed ──next_attempt_at──► pending
   │                                      │
   └──────── canceled ◄───────────────────┴── attempts >= max ──► dead
```

- 退避：`base * 2^(attempts-1)`，加 jitter；`max_attempts` 默认 5。
- 幂等：`dedupe_key` 默认由 repo 写成 `message_id:delivery_index`，保证同一消息内唯一；后续可扩展为业务幂等键。
- 错误脱敏：不把密码、完整 webhook URL、token 写日志/`last_error`。
- 手动重试：只允许 `failed/dead` → 重置 `status=0, next_attempt_at=now()`。

## 7. Channel 适配器

```go
type Sender interface {
    Channel() Channel
    Send(ctx context.Context, d Delivery) (providerMessageID string, err error)
}
```

| Sender | 依赖 | 要点 |
|---|---|---|
| `InboxSender` | 无 | 站内信直接读写 `message_recipients`，可同步完成，不必进 deliveries |
| `MailSender` | SMTP / 邮件服务商 | 发件人、超时、HTML 转义、附件暂不做 |
| `SMSSender` | Twilio / 阿里云 / 腾讯云 | 手机号来源：`users.phone`（空 = 无目标，发送请求直接 400） |
| `WebhookSender` | HTTP | HMAC 签名、超时、重试、响应体截断、**禁止私网地址** |

收件人解析（消费侧窄接口，组合根用 user 表实现）：

```go
type Directory interface {
    Resolve(ctx context.Context, filter RecipientFilter) ([]Contact, error)
}

type Contact struct {
    UserID uint64
    Email  string
    Phone  string
}
```

组合根只查 `status=active` 的用户；`users.phone` 为空时 SMS 渠道没有可用目标，发送请求直接返回 `40000`（不会静默丢消息）。

## 8. 模板渲染

- 文本：`text/template`；HTML：`html/template`（禁止用字符串拼接）。
- `variables` 白名单：渲染前校验 `data` 只包含允许的 key。
- 渲染结果写入 `messages.body` 与 `deliveries.payload` 快照；后续改模板不影响历史消息。
- 模板 key 不存在/变量缺失 → `CodeInvalidArgument`，不创建半成品消息。

## 9. 权限

| 权限 | 角色 | 说明 |
|---|---|---|
| `messages:read` | admin + member | 自己的收件箱/未读数 |
| `messages:send` | admin | 发送、查看已发送 |
| `messages:manage` | admin | 模板、投递记录、手动重试 |

归属校验放 service（`actor` 显式入参）；中间件只做粗粒度 RBAC。

## 10. 前端计划（Phase 4）

- 顶栏铃铛 + 未读红点：轮询 `GET /messages/unread-count`（30–60s）。
- 消息中心 `/messages`：列表、未读筛选、详情、全部已读、删除。
- 管理端 `/messages/send`：收件人选择、渠道勾选、模板选择、预览。
- 模板管理 `/message-templates`。
- 类型全部走 `pnpm gen:api`；不要手写 DTO。

## 11. 配置草案

```yaml
message:
  worker_enabled: true
  poll_interval: 5s
  batch_size: 50
  max_attempts: 5
  retry_base: 30s
  retention_days: 90
  webhook:
    timeout: 10s
    hmac_secret: ""       # 生产必须注入
    allowed_hosts: []     # 空 = 禁止 Webhook
  mail:
    host: ""
    port: 587
    username: ""
    password: ""
    from: ""
  sms:
    provider: ""          # twilio / aliyun / tencent
```

密钥走环境变量/密钥管理，不写配置文件。

## 12. 分阶段计划

### Phase 0：规范与设计（本次）

- [x] `AGENTS.md` 硬性规则 12（goroutine 生命周期）
- [x] `context.md` §2 架构说明 + ADR 14 + §10 扩展指南
- [x] `docs/design-message.md`（本文）
- [x] `README.md` 文档索引

验收：文档评审通过；不改业务代码。

### Phase 1：表 + entity/repo（已完成）

- [x] `migrations/000006_add_phone_to_users.*`
- [x] `migrations/000007_create_message_tables.*`（messages / recipients / deliveries / templates）
- [x] `internal/message/entity.go`
- [x] `internal/message/repository.go`
- [x] `repository_test.go`（SQLite）：唯一约束、软删、收件箱分页、投递领取租约
- [x] `make migrate-up` / `make migrate-down`（真实 PG）验证 000006/000007 up/down

验收：迁移可 up/down；repo 测试覆盖分页、未读过滤、租约领取。

### Phase 2：service + HTTP（站内信 / 发送 / 模板）（已完成）

- [x] `service.go`：Send / Inbox / UnreadCount / MarkRead / MarkAllRead / Delete
- [x] `dto.go` / `handler.go` / `routes.go`：用户侧与管理侧端点
- [x] `authkit`：`messages:read` / `messages:send` / `messages:manage`
- [x] `internal/app` 装配 + `internal/app/integration_test.go`
- [x] `make openapi` + `pnpm gen:api`

说明：邮件/短信/Webhook 本阶段只创建 `message_deliveries` outbox；实际发送在 Phase 3。

验收：匿名 401、member 403、admin 发送/收件箱/已读全链路；`make check` 全绿。

### Phase 3：worker + 多 channel

- [ ] `internal/message/worker.go`：`Run(ctx)`、ticker、租约领取、退避、在途等待
- [ ] `go mod tidy` 将 `golang.org/x/sync` 提升为直接依赖（errgroup）
- [ ] `internal/message/sender.go` + mail/sms/webhook 实现
- [ ] `internal/app` 构造 `MessageWorker()`；`cmd/server` 用 `errgroup` 启动
- [ ] worker 单测：fake clock + fake sender（成功/失败/重试/dead/cancel）
- [ ] `make smoke` 验证进程优雅退出

验收：无 Redis 也能发送；worker 可取消；失败重试与 dead 状态正确；`make check` 全绿。

### Phase 4：前端

- [ ] 消息中心 + 未读红点 + 管理员发送/模板页
- [ ] `pnpm typecheck` / `pnpm build` / `pnpm api:check`

### Phase 5：可选增强

- [ ] 保留期限清理 worker
- [ ] Prometheus 指标（投递成功/失败/延迟）
- [ ] SSE 实时红点（多实例时 Redis Pub/Sub）
- [ ] 跨域事件：`outbox_events` + dispatcher（仅在有 3+ 生产者时）
- [ ] 用户渠道偏好/退订

## 13. 测试策略

- service：fake repo，覆盖收件人校验、模板渲染、幂等、权限归属。
- repository：SQLite，覆盖部分唯一索引、软删、分页、未读统计、租约。
- worker：fake clock + fake sender，覆盖成功、失败重试、指数退避、dead、ctx 取消、在途等待。
- 集成：`app.NewWithDB`，覆盖匿名 401、member 403、admin 发送 → 用户收件箱/已读。
- 真实 PG：`make migrate-up` + `make smoke`；不依赖 Redis。
- 并发/竞态：`go test -race ./...`（`make check` 已包含）。

## 14. 风险与待确认

1. ~~**手机号来源**：是否给 `users` 加 `phone`，还是由发送请求显式提供？~~ **已定：`users.phone VARCHAR(32) NOT NULL DEFAULT ''`（空 = 未设置），Phase 1 已落地。**
2. **Webhook 目标**：只允许配置白名单，还是允许 admin 填 URL？建议 v1 白名单，v2 再开放。
3. **全员广播规模**：当前 fan-out 到 `message_recipients`；用户量超过十万时改成 audience + read receipts。
4. **实时性**：轮询可接受；若要 SSE，多实例必须 Redis Pub/Sub。
5. **模板引擎**：`text/template` / `html/template` 是否满足；若需要更复杂语法再评估。
