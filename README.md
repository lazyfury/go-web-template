# go-web-template

行业标准的 Go Web 单体应用模板（Monolith），开箱即用。

> 技术栈：**Go 1.26 / toolchain 1.27.3 · Gin · GORM · PostgreSQL · JWT + bcrypt · uber-go/zap · viper · golang-migrate · Prometheus · OpenAPI 3.0 / Swagger UI**

## 特性

- **模块化单体**：`internal/platform/*` 叶子包 + `internal/web` 传输层 + 每个业务域一个包（`internal/user`），包内按 handler → service → repository 分文件
- **认证与权限**：邮箱+密码登录 → **JWT access token（15m）+ HttpOnly refresh cookie（滑动 30 天，绝对上限 90 天）**；轮换带 30s 复用宽限期（多标签页不掉线）、超窗重用即吊销全部会话；RBAC `admin`/`member`；`/api/v1` 默认全部要求登录（除登录/刷新），401/403 语义分离
- **统一响应格式**：`{ code, message, data }` + 稳定的业务错误码（`internal/platform/apperr`）
- **配置管理**：默认值 < `configs/config.yaml` < 环境变量（`APP_*` 前缀），支持 `.env`
- **结构化日志**：zap JSON 日志，访问日志携带 request-id、耗时、状态码
- **可观测性**：`/healthz` 存活探针、`/readyz` 就绪探针（含 DB 检查）、`/metrics` Prometheus 指标（HTTP 层 + 登录/刷新/会话吊销/投递/保留期清理的业务指标）
- **健壮性**：panic 恢复、请求超时（ctx deadline）、请求体上限、每 IP 限流（token bucket；多副本可切 Redis 共享桶）、CORS、优雅停机
- **数据库**：GORM + PostgreSQL，连接池参数化、慢查询日志；`golang-migrate` 版本化 SQL 迁移（软删除示例）
- **字典/配置模块**：`dict_groups`（setting）+ `dict_items`（dict array）；消费侧 `GET /api/v1/dicts/:group`，管理侧 `/dict-groups`、`/dict-items`；业务模块可用 `dict.Store.Replace/Get/GetInto` 维护对象/数组型配置
- **消息/通知模块**：站内信收件箱 + 模板 + 邮件/短信/Webhook 投递 outbox（`/api/v1/messages`、`/message-templates`、`/message-deliveries`）；worker 支持租约重试与 SMTP/Twilio/Webhook sender，由 `cmd/server` 统一启停；前端含消息中心、顶栏未读红点、发送/模板/投递管理页
- **文档**：路由元数据（`routekit.Op`）自动生成 OpenAPI 3.0，响应 `data` 强类型；`/openapi.json` 提供契约，`/swagger/index.html` 在线预览
- **前端**：`web/admin/` 配套 Admin 前端模板（Vue 3 + TS + Element Plus + UnoCSS），登录 / 仪表盘 / 用户管理 / 字典管理 / 系统设置 / 消息中心 / 消息管理开箱即用；API 类型由 `docs/openapi.json` 生成
- **工程化**：Makefile、Docker 多阶段构建、docker-compose（DB+迁移+应用）、golangci-lint、单元测试全绿
- **CLI**：Cobra 驱动 `cmd/admin`（`seed`/`show`）与 `cmd/seeding`（`users/dicts/templates/messages` 子命令 + `--only`）
- **数据保留**：`internal/housekeeping` 由 `cmd/server` 启动，按窗口硬删除过期 refresh token / 终态投递记录 / 过期消息（`retention.*` 可配，任务级超时与失败可见）

## 快速开始

前置条件：Go ≥ 1.26、Docker。（`go.mod` 固定 `toolchain go1.27.3`，低于它的工具链会自动切换；想完全本地化就 `GOTOOLCHAIN=local`。）

```bash
# 1. 一键起 postgres + 迁移 + 应用（compose 按生产式默认关闭文档端点）
#    首次会生成 .local/jwt-secret（gitignored）并自动注入 APP_AUTH_JWT_SECRET
make docker-up
# 本地想看 Swagger UI 时（会重建 app 容器）：
# APP_SWAGGER_ENABLED=true make docker-up

# 2. 创建管理员（无公开注册）
APP_SEED_PASSWORD='admin-password' make seed-admin EMAIL=admin@example.com

# 可选：批量创建本地测试假数据（用户/字典/消息模板/站内信）
# 生产环境默认拒绝；测试账号密码由 APP_SEED_PASSWORD 提供
make seed-demo
# 自定义规模：USERS=50 MESSAGES=20 PREFIX=demo make seed-demo

# CLI 工具（Cobra）：查看命令与子命令
go run ./cmd/admin --help
go run ./cmd/admin show --email admin@example.com
go run ./cmd/seeding --help
go run ./cmd/seeding users --users 5 --prefix demo
go run ./cmd/seeding --only dict,templates --prefix demo

# 3. 验证
curl http://localhost:8080/healthz        # {"status":"ok"}
curl http://localhost:8080/readyz         # {"status":"ok"}

# 文档端点开启后：
curl http://localhost:8080/openapi.json       # OpenAPI 3.0 契约
curl http://localhost:8080/swagger/index.html # Swagger UI
```

本地开发（不用 Docker 构建应用镜像）：

```bash
# 终端 1：起数据库并迁移
./scripts/dev.sh                          # 内部会 docker compose 起 postgres + 迁移 + go run

# 或手动分步：
docker compose -f deploy/docker-compose.yml up -d postgres
make migrate-up                           # 执行 migrations/*.sql
make run                                  # 或 make dev（air 热重载）
```

## API 示例

```bash
# 登录（refresh token 由服务端写入 HttpOnly cookie；access token 放 body）
TOKEN=$(curl -s -c cookies.txt -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"admin-password"}' \
  | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
# => 200 {"code":0,"data":{"access_token":"…","token_type":"Bearer","expires_in":900,"user":{...,"role":"admin","permissions":["users:read","users:write","users:delete"]}}}

# 创建用户（需要 users:write；账号必须带初始密码）
curl -X POST http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Alice","email":"alice@example.com","password":"alice-password","role":"member"}'
# => 201 {"code":0,"message":"created","data":{"id":2,...,"role":"member",...}}

# 列表 / 详情 / 更新 / 删除（均需 Bearer token）
curl -H "Authorization: Bearer $TOKEN" 'http://localhost:8080/api/v1/users?page=1&page_size=20&keyword=ali'
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/users/2
curl -X PATCH -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Alicia"}' http://localhost:8080/api/v1/users/2
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/users/2

# 字典/配置：group=setting，items=dict array
curl -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"key":"user.status","name":"User status"}' http://localhost:8080/api/v1/dict-groups
curl -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"key":"1","label":"Active","value":"1","sort":10}' \
  http://localhost:8080/api/v1/dict-groups/$GROUP_ID/items
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/dicts/user.status
# => 200 {"code":0,"data":[{"key":"1","label":"Active","value":"1",...}]}

# 刷新 access token（凭 cookie 轮换；滑动续期 = 活跃即长期在线）
curl -b cookies.txt -c cookies.txt -X POST http://localhost:8080/api/v1/auth/refresh

# 非浏览器客户端（CLI/移动端）：refresh token 走 body/header，不依赖 cookie
curl -X POST http://localhost:8080/api/v1/auth/login -H 'Content-Type: application/json' \
  -H 'X-Refresh-Transport: body' -d '{"email":"admin@example.com","password":"admin-password"}'
# → data.refresh_token；之后：
curl -X POST http://localhost:8080/api/v1/auth/refresh -H "X-Refresh-Token: $REFRESH"

# 登出 / 当前用户 / 改密
curl -X POST -H "Authorization: Bearer $TOKEN" -b cookies.txt http://localhost:8080/api/v1/auth/logout
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/auth/me
curl -X PATCH -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"current_password":"admin-password","new_password":"new-admin-password"}' \
  http://localhost:8080/api/v1/auth/password
```

匿名请求返回 `401 {"code":40100,...}`；已登录但权限不足返回 `403 {"code":40300,...}`。

完整接口定义见 `docs/openapi.json` 或 Swagger UI（`/swagger/index.html`）；两者由路由元数据生成，不需要手写注解。

## 项目结构

```
go-web-template/
├── cmd/server/            # 进程入口（main），只做装配
├── cmd/admin/             # CLI（Cobra）：seed 建/重置管理员、show 查看账号
├── cmd/seeding/           # CLI（Cobra）：批量创建测试数据；子命令 + --only
├── internal/
│   ├── app/               # 组合根：装配、生命周期、全链路集成测试
│   ├── platform/          # 叶子包（无业务，可独立单测）
│   │   ├── apperr/        # 业务错误码与类型化错误
│   │   ├── httpx/         # 统一响应信封 OK/Created/NoContent/Fail
│   │   ├── config/        # 配置模型 + viper 加载（文件/环境变量）
│   │   ├── logx/          # zap logger 构建
│   │   ├── routekit/      # 声明式路由元数据 + OpenAPI 3.0 生成
│   │   ├── authkit/       # JWT 签发/校验 + Principal + RBAC + gin 中间件（叶子包）
│   │   ├── passwd/        # bcrypt 哈希/校验（叶子包）
│   │   └── dbx/           # GORM PostgreSQL 连接池
│   ├── web/               # 紧凑 HTTP 层：engine/中间件/探针/优雅停机（无业务）
│   ├── auth/              # 认证域：登录、refresh 轮换、改密、bootstrap 管理员
│   ├── user/              # 用户域：entity → repository → service → handler → routes
│   ├── dict/              # 字典/配置域：group + items、Store 快捷方法、CRUD 路由
│   ├── message/           # 消息/通知域：收件箱、模板、投递 outbox、worker、sender
│   ├── cli/               # CLI 公共启动（config/logger/db/signal）
│   ├── seed/              # 假数据生成逻辑（可 SQLite 单测）
│   └── testutil/          # 测试共享工具（内存 SQLite，显式传 models）
├── migrations/            # golang-migrate SQL 迁移
├── configs/               # YAML 配置（config.yaml / config.example.yaml）
├── deploy/                # docker-compose.yml
├── docs/                  # openapi.json（生成） + troubleshooting.md
├── scripts/               # dev.sh（一键开发）、rename-module.sh（改模块路径）
├── web/admin/             # Admin 前端模板（Vue 3 + TS + Vite + Element Plus + UnoCSS）
├── Dockerfile             # 多阶段构建（alpine + 静态二进制，镜像约 70MB）
├── Makefile               # build/run/dev/test/lint/openapi/migrate/docker 等
└── .golangci.yml          # golangci-lint 配置
```

## 配置

优先级：**内置默认值 < `configs/config.yaml` < 环境变量**（前缀 `APP_`，`.` 转 `_`）。

| 环境变量 | 说明 | 默认值 |
|---|---|---|
| `APP_PORT` | HTTP 监听端口 | `8080` |
| `APP_ENV` | development / staging / production | `development` |
| `APP_DATABASE_HOST` | 数据库地址 | `127.0.0.1` |
| `APP_DATABASE_PORT` | 数据库端口 | `5432` |
| `APP_DATABASE_USER` / `APP_DATABASE_PASSWORD` | 账号 / 密码 | `postgres` |
| `APP_DATABASE_DBNAME` | 库名 | `go_web_template` |
| `APP_LOG_LEVEL` | debug / info / warn / error | `info` |
| `APP_LOG_FORMAT` | json / console | `json` |
| `APP_RATELIMIT_RPS` / `APP_RATELIMIT_BURST` | 每 IP 限流速率 / 突发 | `100` / `200` |
| `APP_HTTP_REQUEST_TIMEOUT` | 单请求 ctx deadline（`/api/v1`） | `15s` |
| `APP_HTTP_MAX_BODY_BYTES` | 请求体大小上限 | `1048576`（1 MiB） |
| `APP_HTTP_TRUSTED_PROXIES` | 可信反向代理（IP/CIDR，逗号分隔）；**空 = 不信任任何代理**，`X-Forwarded-For` 被忽略 | 空 |
| `APP_CORS_ALLOWED_ORIGINS` | 允许的跨域来源（逗号分隔；**跨域带 cookie 时不能用 `*`**） | `*` |
| `APP_CORS_ALLOW_CREDENTIALS` | 是否允许跨域携带凭据（refresh cookie 必需，需配合具体来源） | `false` |
| `APP_SWAGGER_ENABLED` | 是否暴露 `/openapi.json` 与 Swagger UI | `true`（compose 默认 `false`） |
| `APP_METRICS_ENABLED` | 是否暴露 Prometheus 抓取端点 `/metrics`（含 Go 版本与路由画像） | `true`（compose 默认 `false`） |
| `APP_AUTH_JWT_SECRET` | JWT 签名密钥（HS256，≥32 字节；**生产必填**，且拒绝仓库自带的任何默认值/占位值） | 空（启动失败）；`make docker-up` 自动注入 `.local/jwt-secret` |
| `APP_AUTH_JWT_KEY_ID` | 新签发 token 的 `kid` 头（可选；密钥轮换时用来区分是哪把密钥签的） | 空（不写 `kid`） |
| `APP_AUTH_JWT_RETIRED_SECRETS` | 只验不签的旧密钥（逗号分隔），轮换期间让旧 access token 自然过期；生产同样要求 ≥32 字节 | 空 |
| `APP_AUTH_ACCESS_TTL` | access token 有效期 | `15m` |
| `APP_AUTH_REFRESH_TTL` | refresh 滑动有效期（每次刷新重置） | `720h`（30 天） |
| `APP_AUTH_REFRESH_MAX_LIFETIME` | 会话绝对上限（从首次登录算，到点强制重登） | `2160h`（90 天） |
| `APP_AUTH_REFRESH_REUSE_INTERVAL` | 轮换后旧 token 的容忍窗口（多标签页竞态） | `30s` |
| `APP_AUTH_PASSWORD_RPS` / `APP_AUTH_PASSWORD_BURST` | 改密端点按用户限流（每秒补充 / 突发次数） | `0.2` / `5` |
| `APP_AUTH_VERIFY_CACHE_TTL` | 认证校验缓存（每请求少一次用户行读）；本进程写立即失效，多副本最多滞后一个 TTL；`0` 关闭 | `5s`（上限 `1m`） |
| `APP_AUTH_COOKIE_SECURE` | refresh cookie 是否带 `Secure`（本地 http 调试置 `false`；compose 默认 `true`，`make docker-up` 注入 `false`） | `true` |
| `APP_SEED_PASSWORD` | `cmd/admin` / `make seed-admin` 的管理员密码；`cmd/seeding` 的测试用户密码 | — |

`deploy/docker-compose.yml` 按生产式编排，默认 `APP_SWAGGER_ENABLED=false`、`APP_METRICS_ENABLED=false` 且 `APP_AUTH_COOKIE_SECURE=true`；本地调试用 `make docker-up`（自动注入一次性 JWT 密钥并显式 `APP_AUTH_COOKIE_SECURE=false`），需要文档端点时 `APP_SWAGGER_ENABLED=true make docker-up`。手工 `docker compose up` + 浏览器直连 http 时，同样要自己 `export APP_AUTH_COOKIE_SECURE=false`，否则表现为「登录成功但刷新就掉线」。

跨域说明：`web/admin` 开发期通过 vite 代理 `/api`（同源，无 CORS）；若让前端直连 `:8080`，必须把前端来源写进 `APP_CORS_ALLOWED_ORIGINS`（不能是 `*`）并设置 `APP_CORS_ALLOW_CREDENTIALS=true`，否则登录请求会被 CORS 以 **403 空响应体** 拒绝。

生产环境建议：通过密钥管理注入 `APP_DATABASE_PASSWORD` 等敏感项；将 CORS 收紧为实际域名；`APP_DEBUG=false`、`APP_LOG_FORMAT=json`；**`APP_AUTH_COOKIE_SECURE=true`**（镜像内 `configs/config.yaml` 已是 `true`，本地 http 由 `make dev` / `make run` / compose 注入 `false`，别把 `false` 带进 HTTPS 环境）。

## 常用命令

```bash
make tools          # 安装 pin 版本工具（golangci-lint / air / govulncheck / migrate）
make build          # 编译到 bin/
make run            # 直接运行
make dev            # air 热重载
make test           # 单元测试（内存 SQLite，无需 postgres）
make test-race      # 竞态检测
make coverage-func  # 覆盖率文本摘要
make check          # 提交前唯一门槛：lint + gofmt + vet + race 测试 + build + OpenAPI 黄金文件/依赖 同步
make smoke          # 本地冒烟：探针 + 登录/JWT + 权限 + CRUD + refresh
make verify-auth    # 认证端到端（需 postgres）：宽限期/登出即时/body 传输/会话上限
make audit          # 安全+SQL 机械审计（auth/迁移改动、发布前必跑）
make seed-admin EMAIL=admin@example.com APP_SEED_PASSWORD='...'  # 建/重置管理员
make db-up          # 起本地 postgres（docker compose 优先）
make openapi        # 重新生成 docs/openapi.json（改了路由元数据后）
make migrate-up     # 执行迁移（有 docker 用镜像，否则用本地 migrate）
make migrate-create NAME=add_posts_table   # 新建迁移
make docker-up      # 一键：postgres + migrate + app
make docker-down    # 停止
```

## 测试

```bash
make test
```

测试策略：

- **独立包单测**：`platform/{apperr,httpx,logx,config}` + `web` 中间件（`httptest`）
- **业务单测**：`internal/user`（角色/归属/禁用失效）、`internal/auth`（枚举防护、锁定、refresh 轮换与重用检测、改密失效）、`internal/dict`（key 校验、停用过滤、Replace、GetInto）、`internal/message`（发送/模板/worker/重试）、`internal/seed`（幂等假数据）与 repository（SQLite）
- **认证原语**：`internal/platform/authkit`（HS256 钉死、alg=none、iss/exp、RBAC、中间件 401/403）
- **集成测试**：`internal/app/integration_test.go` 用 `app.NewWithDB` + 内存 SQLite 走通生产同款装配：seed 管理员 → 登录 → bearer → CRUD，并覆盖匿名 401、member 403、refresh 轮换、改密后旧 token 失效
- **OpenAPI 测试**：`internal/platform/routekit/*_test.go`（schema/tag/信封/路径参数/确定性）+ `internal/app/openapi_test.go`（黄金文件、路由完整性、禁用时 404）
- 测试运行无需 PostgreSQL（SQLite 仅用于测试，不进生产镜像）

## 质量门（本地）

仓库不配置远端 CI，提交前在本地跑：

```bash
make check                              # golangci-lint + gofmt + vet + race + build + OpenAPI 黄金文件 + tidy
(cd web/admin && pnpm api:check)        # 校验生成的 API 类型与 docs/openapi.json 同步
(cd web/admin && pnpm build)            # vue-tsc + vite build
```

Docker 端到端：

```bash
make docker-up                      # 默认关闭 /openapi.json、Swagger 与 /metrics；自动注入 JWT 密钥
APP_SWAGGER_ENABLED=true make docker-up   # 本地需要文档时
APP_METRICS_ENABLED=true make docker-up   # 本地要 Prometheus 抓取时
make docker-down
```

直接用 `docker compose` 时 JWT 密钥必须自己注入（compose 里**没有**可用默认值，留空则应用启动即失败）：

```bash
export APP_AUTH_JWT_SECRET="$(openssl rand -base64 48)"
docker compose -f deploy/docker-compose.yml up -d --build
# 忘了 export 的表现：app 容器退出，日志
#   load config: auth.jwt_secret must be at least 32 bytes in production:
#   set APP_AUTH_JWT_SECRET, or run `make docker-up` ...
# 也就是说 `make docker-up`（自动生成并复用 .local/jwt-secret）是本地唯一推荐入口。
```

## 从模板开始你的项目

1. 重命名模块路径（影响 go.mod 与所有 import）：
   ```bash
   ./scripts/rename-module.sh github.com/yourorg/your-project
   ```
2. 按需新增领域：`internal/<domain>/` 内加 entity → 迁移 → repository → service → handler，`routes.go` 用 `routekit.Group` + `Op` 注册路由（不用写 Swagger 注解），最后 `make openapi`；前端类型用 `cd web/admin && pnpm gen:api` 同步。
3. 常用扩展方向：JWT 认证中间件、Redis 缓存、消息队列（Kafka/RabbitMQ）、gRPC 网关、多租户。

## AI 协作文件

本仓库为 AI 代理（及人类协作者）准备了上下文与可复用配方，确保后续改动可靠、风格一致：

- **[AGENTS.md](AGENTS.md)** — Agent 入口：强约束、工作流、文件导航、渐进式披露。
- **[docs/agents/](docs/agents/README.md)** — Agent 分册：workflow / verification / subagents / destructive-operations / commit-and-dod。
- **[context.md](context.md)** — 项目上下文：架构总览、包职责、配置模型、数据模型、API 契约、设计决策（ADR）、扩展指南。
- **[docs/troubleshooting.md](docs/troubleshooting.md)** — 排错速查：环境/配置/迁移/代码结构/linter/前端/容器/鉴权。
- **[docs/design-auth.md](docs/design-auth.md)** — 认证与权限的设计取舍（JWT/refresh/RBAC/威胁模型）。
- **[docs/design-message.md](docs/design-message.md)** — 消息/通知模块设计：SQL 收件箱 + 多 channel 投递 + worker 生命周期与分阶段计划。
- **[skills/](skills/)** — 可复用操作配方（同时以符号链接暴露于 `.agents/skills/` 供自动发现；**会话需从仓库根启动**）：

| skill | 用途 |
|---|---|
| `add-domain` | 新增领域模块（迁移→repo→service→handler→routekit 路由→测试→OpenAPI） |
| `docker-ops` | 容器编排与本地端到端调试（postgres→迁移→后台跑服务→curl 验证；镜像/compose） |
| `database-migrations` | 迁移生命周期管理 |

## License

MIT
