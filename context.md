# context.md — 项目上下文

> 本文件是仓库的「活文档」：架构、数据流、配置、API 契约、设计决策与扩展指南。
> agent 改动代码前先读本文件；改动影响架构/API/配置时，记得同步更新本文件。

## 1. 项目定位

- 行业标准 Go Web **单体应用**模板（HTTP JSON API 服务），非微服务。
- 技术栈：Go 1.26 / Gin / GORM / PostgreSQL / uber-go/zap / viper / golang-migrate / Prometheus / OpenAPI 3.0（routekit 代码生成）/ JWT（golang-jwt v5）+ bcrypt。
- 认证：邮箱+密码登录 → **JWT access token（15m）+ HttpOnly refresh cookie（滑动 30 天 + 绝对上限 90 天，轮换带 30s 复用宽限）**；RBAC `admin`/`member`；`/api/v1` 除 `/auth/login`、`/auth/refresh` 外全部要求登录。
- 模块路径：`github.com/example/go-web-template`（真实项目用 `scripts/rename-module.sh` 替换）。
- **配套前端**：`web/admin/`（Vue 3 + TS + Vite + Element Plus + UnoCSS），真实登录（JWT + refresh cookie）/ 权限化菜单 / 仪表盘 / users CRUD / 字典管理 / 系统设置（基础设置、SEO 设置）/ 消息中心与消息管理（发送、模板、投递记录）；开发时 vite 将 `/api` 代理到 `:8080`。API 类型由 `docs/openapi.json` 生成（`pnpm gen:api`），详见 `web/admin/README.md`。

## 2. 架构总览

```
                       ┌──────────────────────────── HTTP :8080 ─────────────────────────────┐
                       │                                                                     │
  cmd/server/main ───► │  internal/web   (engine + 中间件链 + 探针 + 优雅停机)                │
                       │    │ Recovery → RequestID → Logger → SecurityHeaders → CORS → …      │
                       │    │ /api/v1 组: RateLimit → Timeout(ctx deadline)                  │
                       │    ▼                                                               │
                       │  internal/auth   (登录/JWT/refresh 轮换：同包分文件)      │
                       │  internal/user   (业务一块：handler → service → repository 同包分文件) │
                       │    ▼                                                               │
                       │  internal/platform/{config,logx,dbx,apperr,httpx,authkit,passwd,blobx} 叶子包 ─► PostgreSQL / 对象存储
                       │  internal/platform/routekit（路由元数据 → OpenAPI）              │
                       │                                                                     │
                       │  运维端点: /healthz /readyz /metrics /openapi.json /swagger/*any   │
                       │  （不经限流；OpenAPI 由 routekit 路由元数据在启动时生成）           │
                       └─────────────────────────────────────────────────────────────────────┘
```

- **入口**：`cmd/server/main.go` 只做 flag 解析 → `config.Load` → `logx.New` → `app.New` → `App.Run`（`defer App.Close`）。
- **装配（组合根）**：`internal/app` 手工依赖注入（DB→repo→service→handler→web→server）。`NewWithDB(cfg, logger, db)` 是测试缝：集成测试用同一套生产装配跑内存 SQLite。
- **请求生命周期**：中间件链 → handler 校验 DTO → service 业务规则（依赖 repo 接口）→ repository 执行 SQL → 统一 `httpx.Body` 输出。
- **依赖方向**（禁止反向）：`cmd → app → {web, auth, user, dict, message, file, platform/*}`；`auth → user`（仅单向：auth 通过包内 `UserStore` 接口消费用户数据）；`user/auth/dict → platform/*`；`web → platform/*`。`platform/*` 是叶子包；`web` 不得 import 业务域（域通过 `RegisterRoutes` 自己挂载）。`routekit` 只依赖 `httpx/apperr/gin`，不依赖任何业务域。
- **鉴权装配在组合根**：`app` 用 `api.Group("", routekit.Security("bearerAuth"), routekit.Middleware(authkit.Authenticate(authService)))` 建受保护组，域内再用 `routekit.Middleware(authkit.RequirePermission(...))` 子组声明权限；路由元数据与运行时守卫写在同一次注册里。
- **后台任务边界**：`cmd/server` 是进程生命周期与根 goroutine 的唯一所有者（`signal.NotifyContext` + `errgroup`）；`app` 只负责构造并暴露 worker，**不启动 goroutine**。worker 必须 `Run(ctx) error`、可取消、退出前等待在途任务；禁止在 app/service/handler/repository 里 fire-and-forget。详见 ADR 14 与 `docs/design-message.md`。

## 3. 包职责速查

| 包 | 职责 | 关键文件 |
|---|---|---|
| `cmd/server` | 进程入口 | `main.go`（version/commit ldflags） |
| `cmd/admin` | Cobra CLI：`seed` 建/重置管理员、`show` 查看账号（无公开注册） | `main.go` |
| `cmd/seeding` | Cobra CLI：`users/dicts/templates/messages` 子命令 + `--only`，幂等、生产默认拒绝 | `main.go` |
| `internal/seed` | 假数据生成逻辑（可 SQLite 单测，支持 Only 分片） | `seed.go` |
| `internal/cli` | CLI 公共启动：config/logger/db/signal（cmd/admin、cmd/seeding） | `cli.go` |
| `internal/app` | 组合根：装配 + 生命周期 + 集成测试 | `app.go`, `integration_test.go` |
| `internal/platform/apperr` | 业务错误码 + 类型化错误（纯） | `apperr.go` |
| `internal/platform/httpx` | 统一响应信封 OK/Created/NoContent/Fail | `response.go` |
| `internal/platform/config` | 配置模型 + viper 加载 | `config.go`, `loader.go` |
| `internal/platform/logx` | zap 构建（只依赖字符串参数） | `logger.go` |
| `internal/platform/dbx` | GORM PostgreSQL 连接池 | `db.go` |
| `internal/platform/redisx` | 可选 Redis：客户端 + 共享限流桶（Lua 原子记账）；不是事实来源 | `redis.go`, `ratelimit.go` |
| `internal/platform/routekit` | 声明式路由元数据 + 反射生成 OpenAPI 3.0.3 | `types.go`, `schema.go`, `registry.go`, `group.go`, `serve.go` |
| `internal/web` | 紧凑 HTTP 层：engine/中间件/探针/优雅停机/文档挂载（无业务） | `router.go`, `docs.go`, `middleware_*.go`, `health.go`, `server.go` |
| `internal/user` | 用户域：entity/repo/service/handler/dto/routes 同包分文件 | `entity.go`, `repository.go`, `service.go`, `handler.go`, `dto.go`, `routes.go` |
| `internal/auth` | 认证域：登录、access token 签发、refresh 轮换/吊销、改密、bootstrap 管理员 | `entity.go`, `repository.go`, `service.go`, `handler.go`, `dto.go`, `routes.go` |
| `internal/dict` | 字典/配置域：group=setting，items=dict array；含其他模块用的 `Store` 快捷方法（Replace/Get/GetInto） | `entity.go`, `repository.go`, `service.go`, `values.go`, `handler.go`, `dto.go`, `routes.go` |
| `internal/message` | 消息/通知域：内容、站内信、投递 outbox、模板、HTTP、worker、SMTP/Twilio/Webhook sender | `entity.go`, `repository.go`, `service.go`, `sender.go`, `worker.go`, `handler.go`, `dto.go`, `routes.go` |
| `internal/file` | 文件域：上传/元数据/签名下载/跨模块挂载（`Binder`） | `entity.go`, `repository.go`, `service.go`, `links.go`, `download.go`, `handler.go`, `routes.go` |
| `internal/housekeeping` | 保留期清理 worker：按任务调度 repo 的硬删除，不认识任何业务域 | `worker.go` |
| `internal/platform/authkit` | 叶子包：JWT 签发/校验（HS256 钉死）、Principal、RBAC 表、gin 中间件 | `token.go`, `principal.go`, `rbac.go`, `middleware.go` |
| `internal/platform/passwd` | 叶子包：bcrypt 哈希/校验 + 防枚举的 dummy 比较 | `passwd.go` |
| `internal/platform/blobx` | 叶子包：对象存储抽象（`Store`）+ local/S3 兼容驱动 + 签名/预签名 URL | `store.go`, `local.go`, `s3.go`, `factory.go` |
| `internal/testutil` | 测试共享（内存 SQLite，显式传 models） | `db.go` |
| `migrations/` | golang-migrate SQL（版本化） | `000001`–`000004`（users/auth），`000005`（dict），`000006`（users.phone），`000007_create_message_tables.*`（message），`000008_add_delivery_lease_index.*`（投递领取索引），`000010_create_file_tables.*`（files/file_links） |
| `docs/` | 生成的 OpenAPI 契约 + 手写排错文档 | `openapi.json`, `troubleshooting.md` |

## 4. 配置模型

优先级：**内置默认值 < `configs/config.yaml` < 环境变量（`APP_` 前缀，`.`→`_`）**；`.env` 由 godotenv 预载（可选）。

```yaml
app:      {name, env, debug, port: 8080}
http:     {read_timeout, read_header_timeout, write_timeout, idle_timeout, shutdown_timeout, request_timeout, max_body_bytes, max_header_bytes}
database: {driver: postgres, host, port, user, password, dbname, sslmode, max_open_conns, max_idle_conns, conn_max_lifetime, log_level, slow_threshold: 1s}
log:      {level: info|debug|warn|error, format: json|console}
cors:     {allowed_origins, allowed_methods, allowed_headers, exposed_headers, allow_credentials, max_age}
ratelimit:{enabled, rps, burst, backend: memory|redis}   # backend=redis 时桶在 Redis 共享
redis:    {addr, password, db, pool_size, dial_timeout: 2s, ping_timeout: 1s}
swagger:  {enabled}   # /openapi.json + Swagger UI
auth:     {jwt_secret, issuer, access_ttl: 15m,
           refresh_ttl: 720h,            # 滑动续期（每次 refresh 重置）
           refresh_max_lifetime: 2160h,  # 会话绝对上限（从首次登录算）
           refresh_reuse_interval: 30s,  # 轮换后旧 token 的宽限期（多标签页竞态）
           bcrypt_cost: 12, login_max_attempts: 5, login_lockout: 15m,
           login_rps: 5, login_burst: 10, verify_cache_ttl: 5s, cookie_secure}
message:  {worker_enabled: true, poll_interval: 5s, batch_size: 50, lease: 2m,  # lease >= delivery_timeout（Validate 强制）
          retry_base: 30s, delivery_timeout: 30s,
          webhook_urls: [], webhook_secret, webhook_allow_private: false,
          mail: {host, port: 587, username, password, from, require_tls: true},
          sms:  {provider: twilio, account_sid, auth_token, from, endpoint},
          default_locale: zh-CN, max_attempts: 5}
otel:     {enabled: false, endpoint: 127.0.0.1:4317, insecure: true,
           sample_ratio: 1.0, service_name: ""}   # OpenTelemetry → OTLP gRPC（本地 make observability-up 指向 Tempo）
storage:  {driver: local|s3, max_upload_bytes: 10485760, allowed_mime: [...], allowed_ext: [...],
           inline_mime: [...], signing_secret, orphan_ttl: 24h,
           local: {root: .local/uploads, base_url: http://localhost:8080},
           s3:    {endpoint, region, bucket, access_key, secret_key, use_path_style, presign_ttl: 10m},
           assets: {base_url, cache_ttl: 24h},
           public_token_bytes: 32, upload_rps: 2, upload_burst: 10,
           upload_daily_files: 200, upload_daily_bytes: 104857600}
retention:{enabled: true, interval: 1h, timeout: 1m,
           refresh_token_retention: 168h,   # 过期 refresh token 再留 7 天
           delivery_retention: 720h,        # 终态投递记录保留 30 天（pending 永不删）
           message_retention: 2160h}        # 过期消息再留 90 天（无 expires_at 永不删）
```

- 关键环境变量：`APP_PORT`、`APP_DATABASE_HOST/PORT/USER/PASSWORD/DBNAME`、`APP_LOG_LEVEL/FORMAT`、`APP_RATELIMIT_RPS/BURST`、`APP_HTTP_REQUEST_TIMEOUT`、`APP_HTTP_READ_HEADER_TIMEOUT`（header 阶段超时，防 slowloris）、`APP_HTTP_MAX_BODY_BYTES`、`APP_HTTP_MAX_HEADER_BYTES`（请求头上限，默认 16 KiB；stdlib 默认 1 MiB 太大）、`APP_CORS_ALLOWED_ORIGINS`（逗号分隔）、`APP_SWAGGER_ENABLED`、`APP_METRICS_ENABLED`、`APP_HTTP_TRUSTED_PROXIES`（可信反代 IP/CIDR；空=不信任）、**`APP_AUTH_JWT_SECRET`**（生产必填 ≥32 字节且非模板占位值）、`APP_AUTH_JWT_KEY_ID`/`APP_AUTH_JWT_RETIRED_SECRETS`（密钥轮换：`kid` + 只验不签的旧密钥，逗号分隔）、`APP_AUTH_PASSWORD_RPS/BURST`（改密按用户限流）、`APP_AUTH_VERIFY_CACHE_TTL`（认证校验缓存，默认 5s，0=关闭）、`APP_AUTH_COOKIE_SECURE`、`APP_SEED_PASSWORD`（`cmd/admin` / `cmd/seeding` 读取）、`APP_MESSAGE_WORKER_ENABLED`/`APP_MESSAGE_POLL_INTERVAL`/`APP_MESSAGE_MAX_ATTEMPTS`、`APP_RATELIMIT_BACKEND`（`memory`|`redis`，多副本必须 `redis`）、`APP_REDIS_ADDR`/`APP_REDIS_PASSWORD`/`APP_REDIS_DB`、`APP_OTEL_ENABLED`/`APP_OTEL_ENDPOINT`/`APP_OTEL_INSECURE`/`APP_OTEL_SAMPLE_RATIO`/`APP_OTEL_SERVICE_NAME`（分布式追踪，默认关闭）、`APP_RETENTION_ENABLED`/`APP_RETENTION_INTERVAL`/`APP_RETENTION_REFRESH_TOKEN_RETENTION`/`APP_RETENTION_DELIVERY_RETENTION`/`APP_RETENTION_MESSAGE_RETENTION`（保留期清理，默认开启）、`APP_STORAGE_DRIVER`、`APP_STORAGE_MAX_UPLOAD_BYTES`、**`APP_STORAGE_SIGNING_SECRET`**（本地驱动下载 URL 的 HMAC 密钥，生产必填 ≥32 字节非占位值）、`APP_STORAGE_LOCAL_ROOT`/`APP_STORAGE_LOCAL_BASE_URL`、`APP_STORAGE_S3_ENDPOINT`/`_REGION`/`_BUCKET`/`_ACCESS_KEY`/`_SECRET_KEY`/`_USE_PATH_STYLE`/`_PRESIGN_TTL`（S3 兼容：MinIO / 阿里云 OSS / R2）、`APP_STORAGE_ORPHAN_TTL`、`APP_STORAGE_ASSETS_BASE_URL`/`APP_STORAGE_ASSETS_CACHE_TTL`（长期外链前缀与缓存时间 = 撤销最坏延迟）、`APP_STORAGE_PUBLIC_TOKEN_BYTES`、`APP_STORAGE_UPLOAD_RPS`/`APP_STORAGE_UPLOAD_BURST`/`APP_STORAGE_UPLOAD_DAILY_FILES`/`APP_STORAGE_UPLOAD_DAILY_BYTES`（上传按用户限流 + 24h 配额）。
- `cors.allowed_origins` 在启动时校验（`validateCORS`）：`allow_credentials=true` 时禁止 `*`（含空列表回落到 `*`）、禁止 `https://*.example.com` 这类通配模式（`gin-contrib/cors` 未开 `AllowWildcard`，不会展开），且每项必须是「scheme://host[:port]」形式的 origin（不带 path/query）。这些组合以前能启动成功、但浏览器会静默拒收（refresh cookie 流程直接失效），或让 `cors.New` 在装配期 panic；现在统一变成可读的配置错误。逗号分隔的环境变量会被 trim。
- 邮件/短信/Webhook 凭证（`message.mail.password`、`message.sms.auth_token`、`message.webhook_secret`）必须走环境变量/密钥管理，不要写进镜像配置。
- `APP_AUTH_JWT_SECRET` **显式置空**按「没设置」处理（不会被 viper 忽略后回落到 `configs/config.yaml` 的开发默认值，否则报错会误导成占位值问题）；`auth.jwt_secret` 为空直接启动失败；`app.env=production` 时长度 <32 或命中占位标记（`change_me`/`insecure`/`placeholder`/`example`/`dev-only`/`local-compose-default`/`replace-with`/`your-secret`）→ 启动失败（fail-fast）。**仓库内任何配置文件都不允许携带可用密钥**：`TestRepoConfigSecretsAreRejected` 扫描 `configs/*.yaml`、`.env.example`、`deploy/docker-compose.yml`（含 `${VAR:-default}` 形态）钉死这条；本地编排由 `make docker-up` 生成 `.local/jwt-secret` 注入。
- **签名密钥轮换**（`authkit.TokenIssuer` 的密钥环，首个签字、其余只验）：
  1. 新密钥写入 `APP_AUTH_JWT_SECRET`，`APP_AUTH_JWT_KEY_ID=k2`；
  2. 旧密钥写进 `APP_AUTH_JWT_RETIRED_SECRETS`（逗号分隔）→ 旧 access token 继续可用，**不会全量掉线**；
  3. 等 `auth.access_ttl`（默认 15m）过去，清空 `RETIRED_SECRETS` 完成退役（旧 token 立即 401）。
  `kid` 只是选钥提示，不参与信任判定（伪造 `kid` 只会换来签名不匹配）；退休密钥在 `production` 同样要求 ≥32 字节且非占位值（它仍能验签）。真实验证：`make verify-auth` 的 F 段。
- 所有时长键用字符串（`"10s"`）；viper 默认挂 `StringToTimeDuration` 钩子。
- 显式 `-config` 缺失 → 启动失败；默认路径缺失 → 用默认值 + env。
- 安全演练与已知取舍：见 `docs/security-review-2026-10.md`（F1–F12 的发现、复现命令与修复 commit）。
- 生产建议：密钥走环境变量/密钥管理；`APP_DEBUG=false`、CORS 收紧、`APP_LOG_FORMAT=json`；反代终止 TLS（此时 `SecurityHeaders` 会带 HSTS）。
- `deploy/docker-compose.yml` 是生产式编排，默认 `APP_SWAGGER_ENABLED=false`；本地看文档用 `APP_SWAGGER_ENABLED=true make docker-up`。

## 5. 数据模型与迁移

`users` 表（`migrations/000001_create_users_table.*`）：

| 列 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| name | varchar(64) NOT NULL | |
| email | varchar(255) NOT NULL | 部分唯一索引 `WHERE deleted_at IS NULL`（软删下允许复用邮箱） |
| status | smallint NOT NULL DEFAULT 1 | 1=active 0=disabled（`user.Status`） |
| role | varchar(32) NOT NULL DEFAULT 'member' | `admin` / `member`（`authkit.Role`） |
| password_hash | varchar(255) NOT NULL DEFAULT '' | bcrypt 摘要；空 = 未设凭证，无法登录 |
| token_version | integer NOT NULL DEFAULT 0 | 改密/禁用时 +1 → 旧 access token 立即失效 |
| last_login_at / password_changed_at | timestamptz NULL | 审计 |
| failed_login_attempts | smallint NOT NULL DEFAULT 0 | 连续失败计数（登录成功清零） |
| locked_until | timestamptz NULL | 超过 `auth.login_max_attempts` 后锁定 |
| created_at / updated_at | timestamptz NOT NULL | |
| deleted_at | timestamptz NULL | **软删除**：`Delete` 只置该列，行保留审计 |

`refresh_tokens` 表（`migrations/000003_create_refresh_tokens.*` + `000004_add_refresh_session_start.*`）：

| 列 | 类型 | 说明 |
|---|---|---|
| id / user_id | BIGSERIAL PK / BIGINT FK→users(ON DELETE CASCADE) | |
| token_hash | CHAR(64) UNIQUE | **只存 SHA-256**，原文不落库 |
| expires_at | timestamptz NOT NULL | 滑动：每次轮换续满 `auth.refresh_ttl`，但被 `session_started_at + refresh_max_lifetime` 夹紧 |
| revoked_at | timestamptz NULL | 吊销时间 |
| revoked_reason | varchar(32) NULL | `rotated` / `logout` / `reuse` / `superseded` / `password` / `admin` / `session_cap`。**只有 `rotated` 在 `refresh_reuse_interval` 内可被容忍** |
| session_started_at | timestamptz NOT NULL DEFAULT now() | 跨轮换继承 → 一次登录的会话绝对上限 |
| user_agent / ip / created_at | | 审计 |

轮换时旧行置 `revoked_at=now()` + `reason=rotated`；宽限期外重放按盗用处理并吊销该用户全部会话。

`dict_groups` + `dict_items` 表（`migrations/000005_create_dict_tables.*`）：

| 表 / 列 | 类型 | 说明 |
|---|---|---|
| `dict_groups.id` | BIGSERIAL PK | |
| `group_key` | varchar(64) NOT NULL | **机器码，创建后不可变**；部分唯一索引 `WHERE deleted_at IS NULL` |
| `name` / `description` | varchar(64) / varchar(255) | 展示名与说明 |
| `status` | smallint NOT NULL DEFAULT 1 | 1=启用 0=停用；停用组对消费侧表现为 404 |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | 软删除 |
| `dict_items.id` | BIGSERIAL PK | |
| `group_id` | BIGINT NOT NULL FK→dict_groups(ON DELETE CASCADE) | |
| `item_key` | varchar(64) NOT NULL | 组内唯一（部分唯一索引 `(group_id,item_key) WHERE deleted_at IS NULL`） |
| `label` | varchar(128) NOT NULL | 展示文案 |
| `value` | varchar(1024) NOT NULL DEFAULT '' | 可覆盖 key 的值；空值回落 `item_key` |
| `value_type` | varchar(16) NOT NULL DEFAULT 'string' | `string` / `number` / `boolean` / `json` |
| `sort_order` / `status` / `remark` | integer / smallint / varchar(255) | 排序、启停、备注 |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | 软删除 |

`messages` / `message_recipients` / `message_deliveries` / `message_templates` 表（`migrations/000007_create_message_tables.*`，Phase 1）：

| 表 | 作用 |
|---|---|
| `messages` | 消息内容快照（title/body/type/level/link/audience/template_key/created_by/expires_at，软删） |
| `message_recipients` | 站内信收件箱：`(message_id,user_id)` 唯一，`read_at`/`deleted_at` |
| `message_deliveries` | 邮件/短信/Webhook 投递 outbox：`channel/target/payload/status/attempts/next_attempt_at/locked_until`，`dedupe_key` 部分唯一 |
| `message_templates` | 模板：`(template_key,channel,locale)` 部分唯一（`WHERE deleted_at IS NULL`），`variables` 用 TEXT 存 JSON 数组 |

- 投递领取用 `locked_until` 租约 + 条件更新（SQLite 可测，不依赖 `SKIP LOCKED`）；worker 发送前 `RenewLease` 条件续租，丢掉租约就跳过（防重复投递）。
- `000009` 的 `idx_message_deliveries_purge` 是 `(updated_at) WHERE status IN (2,4,5)` 部分索引：`PurgeTerminalDeliveries` 按 `status IN (2,4,5) AND updated_at < cutoff` 删除终态投递，没有它只能全表扫（真实 PG 16、20 万行 / 47 MB、每小时清理约 1800 行：Seq Scan 31.1ms vs Index Scan 0.5ms）。改清理谓词时同步复核该索引。
- `000008` 的 `idx_message_deliveries_lease` 是 `(locked_until) WHERE status = 1` 部分索引：没有它，「租约过期的在途行」这条腿只能全表扫 `status=1`（真实 PG 16、25 万行 / 5 万在途：Parallel Seq Scan 7.6ms、扫 25 万行；有索引后 Bitmap Index Scan 1.2ms）。改 `ClaimDeliveries` 的谓词时同步复核这个索引。
- 详细状态机、worker 生命周期与 API 计划见 `docs/design-message.md`。

`files` + `file_links` 表（`migrations/000010_create_file_tables.*`）：

| 表 / 列 | 说明 |
|---|---|
| `files.storage`/`bucket`/`object_key` | 对象定位三元组；`object_key` 由服务端生成 `file/YYYYMM/<uuid><ext>`，部分唯一索引 `(storage,bucket,object_key) WHERE deleted_at IS NULL` |
| `files.original_name`/`ext`/`mime_type` | 展示名、扩展名、**服务端嗅探**的 MIME（客户端 Content-Type 不被信任） |
| `files.size_bytes`/`sha256`/`biz_type` | 实际字节数、流式哈希（秒传锚点）、业务类型（avatar/attachment/export…） |
| `files.visibility`/`status`/`owner_id` | 0=私有 1=公开；0=pending 1=ready 2=deleting；上传者（用户删除时 SET NULL） |
| `files.deleted_at` | 软删；查询显式 `deleted_at IS NULL`（`*time.Time` 不触发 GORM 软删作用域） |
| `file_links.(file_id,owner_type,owner_id,slot)` | 跨模块挂载；部分唯一索引 `(owner_type,owner_id,slot,file_id) WHERE deleted_at IS NULL` 保证幂等 |
| `file_shares.(file_id,token,purpose,expires_at,revoked_at)` | 长期公开资产外链：随机 token（32B base64url）、可永久（`expires_at NULL`）、可撤销；`(token) WHERE revoked_at IS NULL` 与 `(file_id,purpose) WHERE revoked_at IS NULL` 两个部分唯一索引 |
| 索引 | `(owner_id,created_at DESC)`、`(status,created_at)`（清理）、`(sha256,size_bytes)`、`(owner_type,owner_id)`、`(file_id)` |

- 删除是两阶段：`status=2(deleting)` → worker 删 blob → 硬删行。顺序反了就留孤儿对象。
- 详细设计与跨模块用法见 `docs/design-file.md`。

- 迁移工具：golang-migrate（SQL 文件），`make migrate-up/down/create`；`make migrate-roundtrip` 在 scratch 库上跑 `up → down -all → up`，用来证明 down SQL 真的可回滚（`audit` 只检查文件成对/非空）。
- **GORM model 与迁移 SQL 是两套事实来源**，改表必须两处同步；测试库用 `AutoMigrate`（`internal/testutil.NewSQLiteDB(t, &user.User{})`）自动建表。
- 新表记住：唯一索引要考虑软删除场景；查询保持 `LOWER()` 可移植写法。
- HTTP 响应使用 `internal/user/dto.go` 的 `UserDTO`/`ListResponse`，GORM entity 不直接作为 API 模型；暴露新字段必须改 DTO，避免 DB 字段意外泄漏。

## 6. API 契约

统一信封：`{"code": <int>, "message": <string>, "data": <any|omitempty>}`；`code=0` 成功。

业务错误码（`internal/platform/apperr`）：`0` 成功；`40000` 参数错误；`40100` 未认证（缺失/无效/过期 token、凭证错误）；`40300` 无权限（角色不足、管理员自删；**登录失败一律 40100**，含锁定/禁用）；`40400` 不存在；`40900` 冲突；`41300` 请求体过大；`42900` 限流；`50000` 内部错误；`50001` 数据库错误。已知码精确映射到 HTTP 状态，未知码按 `[HTTP][类别][序号]` 布局回退。

错误响应额外带 `request_id`（与 `X-Request-ID` 一致，便于客户端关联日志）。OpenAPI 文档由 `routekit.Op` 元数据生成，响应的 `data` 使用真实 DTO 类型（不是 `any`）。

**认证**：`Authorization: Bearer <access token>`（HS256，15m）。
refresh token 走 `HttpOnly; SameSite=Strict` cookie（`gw_refresh`，Path `/api/v1/auth`）；**浏览器不需要任何额外处理就能长期在线**：轮换即滑动续期（默认 30 天），活跃用户永不重登，只有闲置超过 `refresh_ttl` 或会话达到 `refresh_max_lifetime`（默认 90 天）才需要重登。
非浏览器客户端（CLI/移动端）可用 body/header 传输：登录时带 `X-Refresh-Transport: body` 让 `refresh_token` 出现在响应体（不设 cookie），之后用 `X-Refresh-Token` 头刷新。
`refresh_reuse_interval`（默认 30s）容忍同一 cookie jar 的多标签页并发轮换；只有**轮换导致**的吊销会被容忍，登出/改密/重用检测/会话到期**立即生效**（`revoked_reason` 区分）。OpenAPI 通过 `securitySchemes.bearerAuth` 声明，受保护操作自动带 `401`。

| 端点 | 方法 | 登录 | 权限 | 说明 | 成功 | 失败 |
|---|---|---|---|---|---|---|
| `/api/v1/auth/login` | POST | 否 | — | `{email,password}`；按 IP 限流 + 账号锁定（`auth.login_rps/burst`）；失败统一 `401 invalid credentials`（未知邮箱/密码错/锁定/禁用同形，均跑 bcrypt 消除时序差） | 200 `{access_token,token_type,expires_in,user}` + cookie | 400/401 |
| `/api/v1/auth/refresh` | POST | 否（cookie 或 `X-Refresh-Token`） | — | 轮换 + 滑动续期；30s 内重放旧 token 容忍（并清理兄弟 token）；超窗重用 → 吊销全部会话；超 90 天绝对上限 → 401 | 200 同上 | 401 |
| `/api/v1/auth/logout` | POST | 是 | — | 吊销当前 refresh 会话（幂等，立即生效、不受宽限期影响） | 204 | 401 |
| `/api/v1/auth/me` | GET | 是 | — | 当前用户 + 角色 + 权限列表 | 200 `UserView` | 401 |
| `/api/v1/auth/password` | PATCH | 是 | — | `{current_password,new_password}`；`token_version++` 并吊销所有会话；按用户限流（`auth.password_rps/burst`） | 204 | 400/401/429 |
| `/api/v1/users` | GET | 是 | `users:read` | 列表（admin） | 200 | 401/403 |
| `/api/v1/users` | POST | 是 | `users:write` | 建用户（须带初始密码） | 201 | 400/401/403/409 |
| `/api/v1/users/:id` | GET | 是 | `users:read` 或 `users:read:self` | 详情；member 仅本人 | 200 | 400/401/403/404 |
| `/api/v1/users/:id` | PATCH | 是 | `users:write` 或 `users:write:self` | 局部更新；member 仅本人且只能改 name/phone（role/status/password/email 均管理员专属：email 冲突错误会变成邮箱枚举 oracle） | 200 | 400/401/403/404/409 |
| `/api/v1/users/:id` | DELETE | 是 | `users:delete` | 软删除；管理员不能删自己 | 204 | 400/401/403/404 |
| `/api/v1/dicts/:group` | GET | 是 | `dicts:read` | 消费侧取该 setting 的 dict array（仅启用项，按 sort 排序） | 200 `[ItemDTO]` | 400/401/403/404 |
| `/api/v1/dicts/:group` | PUT | 是 | `dicts:write` | 整组替换 setting：`{"value": {...}}` 或 `{"value": [...]}`，语义同 `dict.Store.Replace` | 204 | 400/401/403 |
| `/api/v1/dict-groups` | GET | 是 | `dicts:write` | 分组分页列表（含停用，带 item_count） | 200 `GroupListResponse` | 401/403 |
| `/api/v1/dict-groups` | POST | 是 | `dicts:write` | 新建 setting；`key` 创建后不可改 | 201 `GroupDTO` | 400/401/403/409 |
| `/api/v1/dict-groups/:id` | GET | 是 | `dicts:write` | 分组详情（带 item_count） | 200 `GroupDTO` | 400/401/403/404 |
| `/api/v1/dict-groups/:id` | PATCH | 是 | `dicts:write` | 改 name/description/status | 200 `GroupDTO` | 400/401/403/404 |
| `/api/v1/dict-groups/:id` | DELETE | 是 | `dicts:write` | 软删分组并级联软删 items | 204 | 400/401/403/404 |
| `/api/v1/dict-groups/:id/items` | GET | 是 | `dicts:write` | 组内 items；`include_disabled=true` 含停用项 | 200 `[ItemDTO]` | 400/401/403/404 |
| `/api/v1/dict-groups/:id/items` | POST | 是 | `dicts:write` | 新建 item；同组 `item_key` 唯一 | 201 `ItemDTO` | 400/401/403/404/409 |
| `/api/v1/dict-items/:id` | PATCH | 是 | `dicts:write` | 局部更新 item（key/label/value/value_type/sort/status/remark） | 200 `ItemDTO` | 400/401/403/404/409 |
| `/api/v1/dict-items/:id` | DELETE | 是 | `dicts:write` | 软删除 item | 204 | 400/401/403/404 |
| `/api/v1/messages/inbox` | GET | 是 | `messages:read` | 我的收件箱：page/page_size/unread_only | 200 `InboxResponse` | 400/401/403 |
| `/api/v1/messages/unread-count` | GET | 是 | `messages:read` | 未读数 | 200 `UnreadCountResponse` | 401/403 |
| `/api/v1/messages/inbox/:id` | GET | 是 | `messages:read` | 收件箱详情（不自动已读） | 200 `InboxItemDTO` | 400/401/403/404 |
| `/api/v1/messages/inbox/:id/read` | PATCH | 是 | `messages:read` | 标记已读（幂等） | 204 | 400/401/403/404 |
| `/api/v1/messages/read-all` | POST | 是 | `messages:read` | 全部已读 | 204 | 401/403 |
| `/api/v1/messages/inbox/:id` | DELETE | 是 | `messages:read` | 删除自己的收件记录（软删） | 204 | 400/401/403/404 |
| `/api/v1/messages` | POST | 是 | `messages:send` | 发送：收件人/角色/全员 + channels + 模板 | 201 `SendResultDTO` | 400/401/403 |
| `/api/v1/messages/sent` | GET | 是 | `messages:send` | 已发送列表 + 已读统计 | 200 `SentListResponse` | 401/403 |
| `/api/v1/message-templates` | GET/POST | 是 | `messages:manage` | 模板列表/新建 | 200/201 | 400/401/403/409 |
| `/api/v1/message-templates/:id` | GET/PATCH/DELETE | 是 | `messages:manage` | 模板详情/更新/软删 | 200/204 | 400/401/403/404/409 |
| `/api/v1/message-deliveries` | GET | 是 | `messages:manage` | 外部投递记录（status/channel 过滤） | 200 `DeliveryListResponse` | 400/401/403 |
| `/api/v1/message-deliveries/:id/retry` | POST | 是 | `messages:manage` | 手动重试 failed/dead 投递 | 204 | 400/401/403/404/409 |
| `/api/v1/files` | POST | 是 | `files:write` | `multipart/form-data`：`file` + 可选 `biz_type`/`visibility`；服务端嗅探类型（≤ `storage.max_upload_bytes`，默认 10 MiB）；按用户限流 + 24h 配额 | 201 `StoredFileDTO` | 400/401/403/413/429 |
| `/api/v1/files` | GET | 是 | `files:read` | 分页列表（member 只看自己的；admin 全量）：page/page_size/biz_type/keyword | 200 `Page` | 401/403 |
| `/api/v1/files/:id` | GET | 是 | `files:read` | 元数据；member 仅本人或 public 文件 | 200 `StoredFileDTO` | 400/401/403/404 |
| `/api/v1/files/:id/url` | GET | 是 | `files:read` | 签名下载 URL（`disposition=inline\|attachment`、`ttl_seconds` ≤3600）；`inline` 仅对 `inline_mime` 生效，其余强制降级为 attachment | 200 `URLResponse` | 400/401/403/404 |
| `/api/v1/files/:id/text` | GET | 是 | `files:read` | `text/*` 有界纯文本预览（≤256 KiB，返回 `truncated`）；后台预览用，避免 iframe/CORS | 200 `TextPreviewResponse` | 400/401/403/404 |
| `/api/v1/files/:id` | PATCH | 是 | `files:write` | 改 visibility（owner/admin）；**设为私有会撤销全部公开外链** | 200 `StoredFileDTO` | 400/401/403/404 |
| `/api/v1/files/:id/share` | POST | 是 | `files:write` | 创建/复用长期公开外链（`purpose`、`expires_in_seconds`；同 file+purpose 幂等）；**文件必须 `visibility=public`**，否则 409 | 200 `AssetLink` | 400/401/403/404/409 |
| `/api/v1/files/:id/share` | DELETE | 是 | `files:write` | 撤销外链（`purpose` 为空 = 全部） | 204 | 400/401/403/404 |
| `/api/v1/files/:id` | DELETE | 是 | `files:write` | 软删 + 撤销外链 + 异步清理 blob | 204 | 400/401/403/404 |

- `dicts:read` 授予 admin + member；`dicts:write` 仅 admin。消费侧只返回启用组/项，管理侧可见停用数据。
- `files:read`/`files:write` 授予 admin + member；成员级归属（只能读写自己的）在 service 校验，`public` 文件仅放开读。
- `StoredFileDTO.inline_allowed` 由 `storage.inline_mime` 计算（默认 image/pdf/text/video），后台据此决定「内联预览」还是「只能下载」。
- 私有文件**不能**生成公开外链（`PublicURL`/`Share` 返回 409）：外链绕过认证，只有 `visibility=public` 才允许；改回私有时自动撤销已有外链。私有文件用短签名 URL 临时分享。
  下载 URL 是能力 URL：签名错误一律 404（不确认 id 是否存在），`inline` 仅限 `storage.inline_mime`。
- `messages:read` 授予 admin + member（只能读自己的收件箱）；`messages:send` / `messages:manage` 仅 admin。邮件/短信/Webhook 写入 `message_deliveries` outbox，由 `cmd/server` 启动的 worker 异步发送并重试。
- `ItemDTO.value` 是**存储原值**（空字符串表示未显式设置）；`dict.Store.GetInto` 解码时才回落 `item_key`，避免前端表单回写时把 key 误固化成 value。
- 业务模块内部用 `dict.Store` 的 `Get` / `GetInto` / `Replace`（见 §10）；前端/外部系统用 `GET` / `PUT /api/v1/dicts/:group`。

| 端点 | 方法 | 说明 | 成功 | 失败 |
|---|---|---|---|---|
| `/healthz` | GET | 存活探针（不依赖外部） | 200 | — |
| `/readyz` | GET | 就绪探针（ping DB + 迁移版本校验） | 200 | 503（DB 不可达 / 迁移落后 / dirty / 未初始化，响应带 `reason`；落后时附 `applied_version`/`required_version`） |
| `/metrics` | GET | Prometheus 指标 | 200 | — |
| `/openapi.json` | GET | OpenAPI 3.0 文档（`swagger.enabled` 控制） | 200 | — |
| `/swagger/*any` | GET | Swagger UI，读取 `/openapi.json`（`swagger.enabled` 控制） | 200 | — |
| `/files/:id/:disposition/:exp/:sig` | GET | 本地驱动的签名下载（HMAC 即凭证，不在 `/api/v1`；不进 OpenAPI） | 200（支持 Range） | 404（签名错/过期/已删） |
| `/a/:token/:name` | GET/HEAD | 长期公开资产外链（token 即凭证，不在 `/api/v1`；不进 OpenAPI） | 200（`Cache-Control: public` + ETag/304 + Range） | 404（未知/过期/已撤销） |
中间件对 `/api/v1/*` 生效：先 `RateLimit`，再 `Timeout` 向 `c.Request.Context()` 注入 `http.request_timeout` deadline（service/repository 通过 ctx 传递；handler 不检查 ctx 时不会自动中断，硬超时由 http server 的 read/write timeout 兜底）。运维端点豁免限流。

## 7. 中间件与可观测性

- 链路：`Recovery → RequestID → Logger → SecurityHeaders → CORS → Metrics → BodyLimit → (v1: NoStore → RateLimit → Timeout)`；受保护子组再叠加 `Authenticate → RequirePermission`（`authkit`）。`BodyLimit` 按 `http.max_body_bytes`（默认 1 MiB）拒绝过大请求体。
- `SecurityHeaders`：`X-Content-Type-Options: nosniff` + `X-Frame-Options: DENY` + `Referrer-Policy: no-referrer`，`Strict-Transport-Security` 只在 `app.env=production` 发送（假定 TLS 终止在反代）。
- `NoStore`：`/api/v1/*` 一律 `Cache-Control: no-store`（响应体里有 access token 与用户数据，不得被代理/浏览器缓存）。
- 访问日志在已认证请求上追加 `user_id`（未认证不输出），便于按用户排查；**不记录** `Authorization`/密码/token。query 只记录白名单键（`page`/`page_size`/`status`/`channel`/`level`/`unread`/`read`/`sort`/`order`，单值截断 32 字符），`keyword` 等自由文本与未知键一律丢弃——搜索词可能是 PII。
- `X-Request-ID`：入站复用或生成 UUID；响应头回写；访问日志与错误响应体（`request_id` 字段）均携带；错误响应统一经 `httpx.Fail`/`httpx.Error` 生成。
- 请求体上限按路由：全局 `http.max_body_bytes`（默认 1 MiB）覆盖 JSON；唯一例外是 `POST /api/v1/files`，它用 `storage.max_upload_bytes + 1 MiB`（`web.BodyLimitOverrides`，按 gin 路由模式匹配）。
- 指标：`http_requests_total{method,path,status}`、`http_request_duration_seconds{method,path}`、`http_requests_in_flight`；path 用路由模式（`/api/v1/users/:id`），未匹配路由固定记为 `unmatched`（避免扫描器打爆基数）。另有 `build_info{version,commit}`（值恒 1，区分构建）与 `db_pool_*`（database/sql 池：open/in_use/idle/max_open/wait，由 `dbx.RegisterPoolMetrics` 幂等注册）。
- 本地演示：`make observability-up` 用 compose profile 额外拉起 Prometheus（:9090）与 Grafana（:3000），自动打开 `/metrics`、挂载 `deploy/prometheus/alerts.yml` 与 `deploy/grafana/dashboard.json`；生产式编排仍默认关闭 `/metrics`。
- 追踪（可选，`otel.enabled` 默认关闭）：`otelgin` 为每个请求建 span（探针 `/healthz`/`/readyz`/`/metrics` 排除），GORM 查询（`dbx` 自带 callback，不引 MySQL/ClickHouse 驱动）、webhook 出站（`otelhttp`）、投递与清理 worker 任务都是子 span；`trace_id` 进访问日志、`X-Trace-Id` 进响应头，失败时 `Trace.Search` 可直接按 id 定位。默认 OTLP gRPC → `otel.endpoint`；本地 `make observability-up` 指向 Tempo，生产可换任意 OTLP 后端。采样 `otel.sample_ratio`（head-based，ParentBased）。
- 业务指标（`/metrics` 默认开启，`metrics.enabled=false` 关闭）：`auth_login_total{result}`、`auth_refresh_total{result}`、`auth_session_revocations_total{reason}`、`auth_account_lockouts_total`（认证域，reason 由 repo 的吊销咽喉点打出）、`message_delivery_attempts_total{channel,result}`、`message_delivery_duration_seconds{channel}`（投递 worker）、`housekeeping_purged_rows_total{task}`、`housekeeping_task_failures_total{task}`、`file_uploads_total{result}`、`file_upload_bytes_total{mime_group}`、`file_asset_requests_total{result}`（外链请求=CDN 未命中）、`file_storage_operations_total{driver,operation,result}`、`file_storage_operation_duration_seconds{driver,operation}`（保留期清理）、`worker_last_success_timestamp_seconds{worker}`（两个 worker 共用，`internal/platform/workerx`：只要一轮完整跑完就刷新；失败轮次不刷新，清理任务失败轮次也不刷新）。标签都是闭集，不含用户/邮箱/目标地址。
- 限流：每 IP token bucket（`x/time/rate`），X-Forwarded-For 取第一跳；超限 429 + `Retry-After`（按桶速率算，`ceil(1/rps)` 秒，上限 60；不再是常数 1）。桶默认在进程内存（`ratelimit.backend=memory`），多副本切 `backend=redis`（Lua 脚本原子记账，时间取 Redis `TIME`，空桶靠 `PEXPIRE` 回收）。存储不可用时**fail-open** + `ratelimit_store_errors_total` 计数 + 限频日志：限流是保护层，不是放行权威。
- 超时：`Timeout` 只注入 context deadline，不另起 goroutine、不伪造响应（`gin.Context` 非并发安全，goroutine 版会 data race）。需要硬拒绝时在 handler 内检查 `ctx.Err()` 并返回业务错误。
- 优雅停机：SIGINT/SIGTERM → `signal.NotifyContext` → `http.Server.Shutdown`（`http.shutdown_timeout` 上限）。
- 消息 worker 日志只记录 `delivery_id`/`channel`/`attempt`，不记录收件地址、payload、密钥。

## 8. 测试策略

- 独立包单测（不依赖 gin/gorm）：`platform/apperr`（码表/`From`/`Unwrap`）、`platform/httpx`（信封与错误映射）、`platform/logx`、`platform/config`（优先级/校验）。
- 业务单测：`internal/user/service_test.go`（fake `Repository`：角色/归属/禁用撤销 token_version）、`internal/auth/service_test.go`（fake `UserStore`+`Repository`+固定时钟：枚举防护、锁定、轮换、重用检测、改密失效）、`internal/dict/service_test.go`（fake `Repository`：key 校验、停用过滤、Replace 整组替换、GetInto 类型解码）、`internal/message/service_test.go`（fake `Repository`+`Directory`：audience 校验、多 channel 草稿、模板渲染、管理员权限）。
- 认证原语：`internal/platform/authkit/authkit_test.go`（HS256 钉死、iss/exp、alg=none、typ 混淆、错误密钥、RBAC 表、中间件 401/403）。
- 文件域：`internal/file/service_test.go`（fake repo + fake store：类型嗅探/大小/归属/签名 TTL/清理/挂载）、`internal/file/repository_test.go`（SQLite：软删过滤、部分唯一索引幂等、孤儿查询）、`internal/platform/blobx/*_test.go`（local 穿越/原子写/签名；s3 用 httptest 假端点）。
- 仓储：`internal/user/repository_test.go`、`internal/dict/repository_test.go`、`internal/message/repository_test.go` 直接对 SQLite 测（软删、部分唯一索引、排序、收件箱分页、投递租约/重试）。
- 传输层：`internal/web/*_test.go`（RequestID/CORS/RateLimit/Timeout/Recovery/Metrics/SecurityHeaders/NoStore/Health，`httptest` 驱动）；`internal/app` 另有一条接线断言（安全头与 `no-store` 真的挂在 router 上）。
- 集成：`internal/app/integration_test.go`（`package app_test`）用 `app.NewWithDB` + 内存 SQLite 走**生产同款装配**：`app.SeedAdmin` → login → bearer → CRUD，覆盖匿名 401、member 403/越权、refresh 轮换与旧 cookie 重用、改密后旧 token 失效、消息收件箱/多 channel 投递/模板渲染、文件上传/签名下载（含 Range 与篡改签名 404）/janitor 清理。
- 路由矩阵：`internal/app/route_matrix_test.go` 给**每一个**已注册路由声明「匿名 401 / member 结果 / admin 结果 / 非法 JSON 400」四列，并由 `TestRouteMatrixCoversEveryRegisteredRoute` 保证新路由必须分类（漏写 = 红）。这是 401/403 契约的防漂移钉子，不靠覆盖率数字兜底。
- 覆盖率门：`make cover-check`（口径 `COVERPKG=./internal/...`）并入 `make check`，由 `scripts/cover-check.sh` 执行三层规则：总量 `COVER_MIN`（默认 78）、每包下限 `COVER_PKG_MIN`（默认 55，`internal/cli` 这类二进制胶水显式豁免）、关键文件（auth/限流/sender/用户仓储）不允许出现 0% 覆盖的函数。总量会被集成测试拉高，单包和关键文件的规则才是防「没人测」的钉子。
- OpenAPI：`internal/platform/routekit/*_test.go`（schema/tag/信封/路径参数/确定性）+ `internal/app/openapi_test.go`（黄金文件、`engine.Routes()` 完整性、禁用时 404、**受保护操作必须带 bearerAuth+401/403，公开端点不得带 security**）。
- 前端：`web/admin` 用 `pnpm api:check` 校验生成的 API 类型未漂移；`pnpm lint`（ESLint 10 + eslint-plugin-vue essential）、`pnpm test`（vitest + jsdom：`tabs` store 持久化/去重、菜单与路由权限一致性、路由守卫的 401/权限重定向、退出登录「先清态再导航」的顺序、`safeRedirectTarget` 与 401 语义表）、`pnpm typecheck`/`pnpm build`（vue-tsc + vite）。一条命令：`make web-check`。
- 测试不依赖 Docker/PostgreSQL；sqlite 驱动仅测试可见；每个测试用独立命名内存库，可并行。
- 跑法：`make check`（提交前唯一门槛）；纯 Go 改动最快闭环 `make test`；端到端 `make smoke`（需 postgres）。
- 性能与健壮性：`make bench`（JWT 签发/校验、限流中间件，`-benchmem`）与 `make fuzz`（`FuzzTokenParse`：任意字符串不得 panic，且只有本 issuer 签发的 token 能通过校验）。热路径改动后跑前者对比 ns/op 与 allocs。

## 9. 关键设计决策（ADR 精简版）

1. **Gin + GORM + zap + viper**：生态最大、招聘/维护成本最低，模板面向大多数团队。
2. **接口在消费侧**：service 依赖 repo 接口（handler 依赖具体 `*Service`，Go 惯例 return structs / accept interfaces）→ 业务单测不碰真实 DB，实现可替换。
3. **统一信封 + 业务码**：前端/网关可稳定判断错误；`Error.Is` 按 Code 匹配（见 AGENTS.md）。
4. **软删除**：默认保留数据，唯一索引按 `deleted_at IS NULL` 部分索引保证邮箱可复用。
5. **SQLite 仅测试**：生产只用 PostgreSQL；测试用 `file::memory:?cache=shared` 内存库。
6. **手工 DI**：规模小，wire/fx 引入的复杂度不值当；未来可演进。
7. **promhttp 直挂**：`/metrics` 用 `promhttp.Handler()`，不依赖 DefaultServeMux 自动注册。
8. **迁移与 model 双源**：SQL 迁移管生产 schema，AutoMigrate 只服务测试，避免启动时隐式改库。
9. **平台包 + 垂直切片（模块化单体）**：`platform/*` 为无业务叶子包（可独立单测），`web` 为无业务的传输层，每个业务域一个包（`user/`）内部按文件分层。包是**部署/测试边界**，文件是**代码分层**；不再为单个域维护 model/repository/service/handler 四个包。`web` 与域之间通过 `*routekit.Group` + `RegisterRoutes` 组合根挂载，避免循环依赖。
10. **OpenAPI 代码优先（routekit）**：路由与 `routekit.Op` 元数据在同一次注册中登记，启动时反射生成 OpenAPI 3.0.3；不再使用 swag 注解。`httpx.Body` 的信封与 `apperr.Code` 的 HTTP 映射自动复用，响应 `data` 有真实类型。`docs/openapi.json` 是黄金文件，`TestOpenAPIGolden` 校验漂移；`TestOpenAPICoversAllRoutes` 保证 gin 路由与文档一一对应。选择 3.0.3 是为了兼容 Swagger UI 与主流客户端生成器。
11. **JWT（HS256）+ 可吊销 refresh token + RBAC**：access token 无状态、15m、带 `ver`（users.token_version），改密/禁用即失效；refresh 为 32B 随机串、只存 SHA-256、每次轮换。**轮换是滑动的**（每次续满 `refresh_ttl`=30d），因此"活跃即长期在线"；`session_started_at` 跨轮换继承，把一次会话硬性封顶在 `refresh_max_lifetime`=90d。`refresh_reuse_interval`=30s 容忍多标签页并发轮换（否则一个标签页刷新会把另一个踢下线），容忍范围仅限 `revoked_reason=rotated`；登出/改密/禁用/重用检测/到期都是立即吊销。签名算法、issuer、过期时间在签发/校验两侧硬编码（`jwt.WithValidMethods`），杜绝 `alg=none`/算法混淆。refresh 放 `HttpOnly+SameSite=Strict` cookie、access 只存内存（前端不落 localStorage），降低 XSS 影响面。RBAC 用代码内静态表（admin/member → 权限字符串）：粗粒度守卫在 `authkit` 中间件，资源归属（本人）在 service，两处职责不重叠。
12. **字典/配置模型（group = setting + items = dict array）**：`dict_groups` 保存 setting 元数据（`group_key` 不可变），`dict_items` 保存有序数组元素（`item_key`、`label`、`value`、`value_type`、`sort_order`）。消费侧 `GET /dicts/:group` 只返回启用项；管理端按 group/item 做 CRUD。其他业务模块通过 `dict.Store` 的 `Replace("seo", map)` / `Replace("keywords", []any)` 做整组替换（事务内 upsert group + purge/insert items），`GetInto` 按 `value_type` 解码到 map/slice/struct。这样模板既支持"下拉字典"，也支持"对象/数组型 setting"，且不把 JSON blob 塞进单列。
13. **前端 TS 6.0 + `ignoreDeprecations: "6.0"`**：`web/admin` 锁 `typescript ^6.0.3`（实测 vue-tsc 2.2.12 兼容，typecheck/build 全绿）。`ignoreDeprecations` 只接受当前 major 对应的值：TS 5.x 仅认 `"5.0"`（写 `"6.0"` 报 TS5103），TS 6.0/7.0 认 `"6.0"`。升级 TS 大版本时需同步调整该值，先改 tsconfig 再升依赖。
14. **进程生命周期与后台 goroutine**：`cmd/server/main.go` 持有 `signal.NotifyContext`，用 `errgroup.WithContext` 同时运行 HTTP、后台 worker 与限流器清扫；任一失败取消另一个，全部退出后才 `App.Close` 释放 DB。`internal/app` 只装配（构造 repo/service/worker）并通过 `App` 暴露 worker，**禁止在 app 内启动 goroutine**；service/handler 只写投递记录（delivery/outbox），异步执行交给 worker。限流器同样遵守这条：`web.NewLimiters()` 是惰性构造，空闲桶清扫由 `App.Limiters()` → `cmd/server` 的 `errgroup` 调用 `Run(ctx)`（旧实现把 `cleanupLoop` 藏在构造函数里，goroutine 无 owner，测试也无法断言）。Worker 契约：`Run(ctx) error`、ticker/clock 可注入、ctx 取消后停止领取新任务并等待在途任务、禁止裸 `go func()` 与 `time.Sleep` 轮询。多实例用 `locked_until` 租约 + 条件更新领取任务（保持 SQLite 可测，不依赖 `SKIP LOCKED`）。

15. **消息/通知 SQL-first（无通用 event bus）**：`messages` 存内容快照，`message_recipients` 是站内信收件箱与已读状态，`message_deliveries` 是邮件/短信/Webhook 的投递 outbox，`message_templates` 存模板。发送在一事务内写 message + recipients + deliveries；`message.Worker` 由 `cmd/server` 通过 errgroup 启动，领取 `message_deliveries` 并调用 SMTP/Twilio/Webhook sender，失败按指数退避重试（failed → 再次领取），超过 `max_attempts` 变 dead。不做通用 event bus；未来跨域事件用 `outbox_events` + dispatcher。Redis 只作为可选加速层，不是消息事实来源。详见 `docs/design-message.md`。

16. **Redis 是可选的跨副本加速层**：`ratelimit.backend=memory|redis`，默认 memory（模板只要求 PostgreSQL 就能跑）。切到 redis 后 token bucket 在 Lua 里原子记账、时间取 Redis `TIME`（唯一所有副本都认的钟）、空桶用 `PEXPIRE` 回收，因此 N 个副本等价于一个限流器。启动时 `PING` 失败直接退出（不允许静默退化成每副本一份额度）；运行期存储不可用则 fail-open + 指标 + 限频日志，且账号锁定仍在 DB（`users.locked_until`），认证安全不依赖 Redis。`/readyz` 不探 Redis：它挂掉只影响限流精度，不该摘掉全部流量。
17. **认证校验缓存是「本进程写立即失效」型**：`auth.Service.VerifyAccess` 每次认证请求都要读用户的 `status/role/token_version`，默认用 `auth.verify_cache_ttl=5s` 的本地缓存摊掉这次读（`internal/auth/verify_cache.go`）。正确性靠**写路径显式失效**保障：`auth.Service`（改密/seed）与 `user.Service`（Update 的任意分支、Delete）都调用 `InvalidateAuthState`，所以单副本下「改密/禁用/删除/改角色 → 下一次请求即生效」仍然成立。跨副本只能靠 TTL：另一台机器最多滞后 `verify_cache_ttl`（上限 1m，超过启动失败）。失败/不存在**不缓存**（不做负缓存，避免把临时 DB 错误放大成一个 TTL），条目数上限 8192 且到期惰性删除。置 0 即关闭缓存。代价写清楚：这是「多一次 DB 读」与「跨副本吊销窗口」的显式交易，不是免费的。

18. **文件存储：元数据在库、字节在 blobx、两种下载通道**：`files` 只存对象定位与展示元数据，字节在 `blobx.Store` 背后（local / S3 兼容）。上传由服务端嗅探真实 MIME（不信任客户端 Content-Type）、按白名单与大小上限校验、流式写存储并同时算 sha256；落库失败会回滚 blob。删除是两阶段（`status=deleting` → worker 删 blob → 硬删行），避免留下无法定位的孤儿对象。私有下载「先换短期签名 URL」：本地驱动是 HMAC 能力 URL（`/files/{id}/{disposition}/{exp}/{sig}`，签名错误一律 404），S3 驱动是预签名 GET——API 不代理字节，也不把对象 key/桶暴露给客户端。跨模块引用只走 `file_links` + `file.Binder`（`owner_type/owner_id/slot`），新模块接入不需要迁移，也不需要 import 文件域的 repository。**两种链接，不混用**：私有文件用短签名 URL（无状态、≤1h、不可撤销）；文章封面/头像/商品图这类需要长期嵌入 `<img src>` 的资产用 `file_shares` 随机 token（`GET /a/{token}/{name}`，可永久、可撤销、匿名可读、`Cache-Control: public` + ETag 让 CDN 吸收流量，缓存 TTL 即撤销最坏延迟）。业务表只存 `file_id`，URL 在渲染期解析，绝不落库。上传侧按用户限流 + 24h 次数/字节配额（`storage.upload_*`），并继续做 MIME 嗅探与大小/类型白名单。浏览器直传 OSS 仍未开放（需要临时凭证交换 + 上传后校验）。详见 `docs/design-file.md`。

## 10. 扩展指南

**新增领域模块**（详见 `.agents/skills/add-domain/SKILL.md`）：迁移 → `internal/<domain>/entity.go` → `repository.go`（接口+GORM 实现）→ `service.go` → `handler.go`（DTO+校验）→ `routes.go`（`RegisterRoutes(*routekit.Group, *Handler)` + `routekit.Op`）→ `app.NewWithDB` 装配 → 单测 + 集成测试 → `make openapi` → 前端 `cd web/admin && pnpm gen:api`。新增端点必须走 `routekit.Group`，不要直接挂 `*gin.RouterGroup`。

**新增受保护端点**：把域挂到 `app.go` 的 `protected` 组（已带 `Security(bearerAuth)` + `Authenticate`），域内用 `rg.Group("", routekit.Middleware(authkit.RequirePermission(...)))` 声明权限；`Op.Errors` 里补 `CodeForbidden`（401 由 routekit 依据 security 自动补）。handler 用 `authkit.PrincipalFrom(c)` 取调用者，归属校验放 service。新增权限 → `authkit.Permission` 常量 + `rolePermissions` 表 + OpenAPI 测试的白名单/断言。

**使用字典快捷方法**（业务模块依赖 `dict.Store`，由组合根注入 `*dict.Service`）：

```go
// 对象型 setting：字段 → items（key=字段名，label=字段名，value=字段值）
_ = store.Replace(ctx, "site.seo", map[string]any{"title": "Example", "robots": "index"})

// 数组型 setting：元素按顺序落成 items，sort_order=下标
_ = store.Replace(ctx, "keywords", []any{"go", "gin", "gorm"})

// 读：map / slice / struct 均可（按 value_type 解码；空 value 回落 key）
var seo struct {
    Title  string `json:"title"`
    Robots string `json:"robots"`
}
_ = store.GetInto(ctx, "site.seo", &seo)

items, _ := store.Get(ctx, "user.status") // []dict.Item，仅启用项
```

- `Replace` 的组不存在时自动创建（name=group_key，status=启用）；已存在则保留管理端维护的 name/description/status。
- 整组替换会在同一事务里 purge 旧 items 再插入新 items；需要在同一事务里做业务写入时，把 `dict.Store` 调用放进调用方事务（当前接口未暴露 `*gorm.DB`，跨模块强一致需后续扩展 `Tx`）。
- 不要绕过 service 直接写 `dict_*` 表；权限（`dicts:read/write`）只在 HTTP 适配层生效，内部调用是可信的。

**其他模块引用文件（file.Binder）**：在消费侧声明窄接口（`Attach/Detach/ListByOwner/DeleteByOwner/SignedURL`），由组合根注入 `*file.Service`；多附件用 `slot=""`，单值槽位用 `slot="avatar"` 这类名字。**不要**把下载 URL 存进业务表（URL 会过期/换驱动失效），也不要直接写 `files`/`file_links` 表。资源删除时调 `DeleteByOwner`；未引用文件由 janitor 按 `storage.orphan_ttl` 清理。`SignedURL` 是可信内部 API，不能接用户传来的 id。

**其他模块渲染长期外链**：`file.Binder.PublicURL(ctx, fileID, purpose)`（幂等，同一 `(file, purpose)` 复用 token）→ 把返回的 `/a/{token}/{name}` 直接放进 HTML；资源删除/换图时调 `RevokeSharesForFile`。**不要把外链或签名 URL 存进业务表**。缓存 TTL 内已撤销的链接仍可能被 CDN/浏览器命中，需要秒级下架时把 `storage.assets.cache_ttl` 调小或用 CDN purge。

**新增存储驱动**：实现 `blobx.Store`（`Driver/Put/Open/Stat/Delete/URL`）→ 在 `blobx.New` 加分支 → `files.storage` 记录驱动名，历史行继续由旧驱动解析。

**新增配置项**：`config.go` 加字段 → `loader.go` `setDefaults` 加默认值 → `config.example.yaml`/`config.yaml` 同步 → 必要时加 Validate 分支。

**新增中间件**：写 `internal/web/middleware_<name>.go`（`package web`，gin.HandlerFunc）→ `internal/web/router.go` 挂链（顺序：Recovery 最外、RequestID 先于 Logger）+ 补 `httptest` 单测。需要按 key 限流时用 `limiters.Middleware(enabled, keyFn, rps, burst)`（组合根创建 registry），不要在中间件/构造函数里起 goroutine。

**新增后台任务/worker**：worker 实现放业务包（如 `internal/message/worker.go`、`internal/housekeeping/worker.go`），只暴露 `NewWorker(...)` + `Run(ctx) error`；在 `internal/app` 构造并通过 `App` 暴露，由 `cmd/server` 用 `errgroup` 启动。不得在 `app.New`/`app.Run` 里 `go`；需要周期任务用 `time.Ticker` + `select ctx.Done()`，需要并发时用有界 pool 并在 `Run` 返回前 `Wait`。详细设计见 `docs/design-message.md`。

**新增运维端点**：放 `internal/web/router.go` 顶层组（豁免限流）；探针逻辑在 `internal/web/health.go`。

**重置管理员/新增账号**：`EMAIL=... APP_SEED_PASSWORD='...' make seed-admin`（或容器内 `/usr/local/bin/admin`）。无公开注册流程；新建用户由 `POST /api/v1/users`（需 `users:write`）完成，必须带初始密码。

## 11. 已知限制

- 限流桶默认是单实例内存态；多副本必须把 `ratelimit.backend` 切成 `redis`（`APP_REDIS_ADDR`），否则每个副本各有一份额度，实际限额 = 配置值 × 副本数。登录**锁定**状态本来就在 `users.locked_until`（DB），天然多副本一致；Redis 只承载限流桶。
- Redis 只做跨副本加速层，不是事实来源：`/readyz` 只探 DB 与迁移版本，Redis 抖动不会摘流量，限流按 fail-open 处理（见 §8 中间件）。
- `/readyz` 的迁移校验只比对**版本号与 dirty 位**（`dbx.RequiredSchemaVersion`，与 `migrations/*.up.sql` 的最大编号由单测钉死），不做列/索引级 diff；`schema_migrations` 表不存在但核心表存在时按「AutoMigrate/其它工具建库」放行（测试与本地库即如此），两者都不存在才 503。
- access token 无状态：吊销/禁用最坏延迟 = `access_ttl`（15m）；改密/禁用通过 `token_version` 即时生效。
- 多标签页共享一个 cookie jar：`refresh_reuse_interval` 期间旧 token 重放会被容忍并只保留最新 token；窗口外仍会判定为盗用并全量吊销（默认 30s，可按需调大）。
- 会话寿命 = `min(闲置 refresh_ttl, 自登录起 refresh_max_lifetime)`：想"更久不用重登"就调这两个值，不要调 `access_ttl`。
- `VerifyAccess` 默认通过 `auth.verify_cache_ttl`（默认 5s）缓存用户的 status/role/token_version：单副本内任何写（改密/改角色/禁用/删除）都会**立即失效**，多副本下另一台最多滞后一个 TTL（置 0 关闭缓存，回到每请求一次 DB 读）。
- HS256 单密钥体系 + `kid` 密钥环轮换：新密钥签、旧密钥（`auth.jwt_retired_secrets`）只验不签，因此轮换**不再全量掉线**；代价是必须记得在 `auth.access_ttl` 之后把退休密钥删掉。缺失的只有非对称算法（RS256/ES256）与 JWKS 端点。
- 无忘记密码/邮箱验证流程：密码只能由管理员 PATCH 重置或本人 `PATCH /auth/password`。
- `Timeout` 只注入 ctx deadline：handler 若不检查 ctx，响应仍会返回成功；需要保证中断的路径必须显式处理 `ctx.Err()`。
- 测试库为每测试独立的命名内存 SQLite（`internal/testutil`），可并行；但仍需避免依赖真实 PostgreSQL 特性。
- OpenAPI 反射只覆盖明确子集：query 数组/对象参数、`*any` 通配路径、自定义 `MarshalJSON`、复杂 `oneof` 尚未支持；不支持的类型会让启动失败，需调整 DTO 或后续扩展显式 schema 覆盖。
- 保留期清理只在 `retention.enabled=true` 时运行，且是**硬删除**：窗口内需要取证的记录请调大窗口或关闭该任务；多副本同时跑 janitor 是幂等的（DELETE 按条件），但会产生重复的空扫描。
- 文件模块：本地驱动（`storage.driver=local`）把字节写在单机目录，多副本必须共享卷或切 `s3`；`visibility=public` 仅表示「所有登录用户可读」，**匿名可读只通过 `/a/{token}` 外链**；外链撤销在缓存 TTL 内可能仍被命中；浏览器直传 OSS 需要临时凭证交换端点，本期仍走服务端中转（受 `storage.max_upload_bytes`、按用户限流与 24h 配额约束）。
- `storage.signing_secret` 轮换会让**已签发未过期**的本地下载链接立即 404（S3 驱动不受影响）。
- `docs/openapi.json` 由 `make openapi` 更新；手工修改会在 `make check` 的黄金测试中失败。

> 报错速查、环境陷阱、linter/前端/容器排错：见 **[docs/troubleshooting.md](docs/troubleshooting.md)**（易变内容不写在本文件）。
