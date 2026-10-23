# context.md — 项目上下文

> 本文件是仓库的「活文档」：架构、数据流、配置、API 契约、设计决策与扩展指南。
> agent 改动代码前先读本文件；改动影响架构/API/配置时，记得同步更新本文件。

## 1. 项目定位

- 行业标准 Go Web **单体应用**模板（HTTP JSON API 服务），非微服务。
- 技术栈：Go 1.26 / Gin / GORM / PostgreSQL / uber-go/zap / viper / golang-migrate / Prometheus / Swagger。
- 模块路径：`github.com/example/go-web-template`（真实项目用 `scripts/rename-module.sh` 替换）。
- **配套前端**：`web/admin/`（Vue 3 + TS + Vite + Element Plus + UnoCSS），登录（mock 可切换真实接口）/ 仪表盘 / users CRUD；开发时 vite 将 `/api` 代理到 `:8080`。详见 `web/admin/README.md`。

## 2. 架构总览

```
                    ┌─────────────────────────────── HTTP :8080 ───────────────────────────────┐
                    │                                                                           │
 cmd/server/main ──►│  internal/router  (路由表 + 中间件链)                                       │
                    │   │  Recovery → RequestID → Logger → CORS → Metrics ─┐                    │
                    │   │  /api/v1 组追加: RateLimit → Timeout              │                    │
                    │   ▼                                                 ▼                    │
                    │  internal/handler  ──►  internal/service  ──►  internal/repository ──► PostgreSQL
                    │  （参数校验/响应）       （业务规则）             （GORM 数据访问）            │
                    │                                                                           │
                    │  运维端点: /healthz /readyz /metrics /swagger/*any（不经限流）             │
                    └───────────────────────────────────────────────────────────────────────────┘
```

- **入口**：`cmd/server/main.go` 只做 flag 解析 → `config.Load` → `app.NewLogger` → `app.Run`（装配 + 优雅停机）。
- **装配**：`internal/app/app.go` 手工依赖注入（DB→repo→service→handler→router→server）；规模扩大后可按需引入 wire/fx，当前刻意保持零魔法。
- **请求生命周期**：中间件链 → handler 校验 DTO → service 业务规则（依赖 repo 接口）→ repository 执行 SQL → 统一 `response.Body` 输出。

## 3. 包职责速查

| 包 | 职责 | 关键文件 |
|---|---|---|
| `cmd/server` | 进程入口 | `main.go`（含 Swagger 注解、version/commit ldflags） |
| `internal/app` | 装配 + logger 构建 | `app.go`, `logger.go` |
| `internal/config` | 配置模型 + viper 加载 | `config.go`, `loader.go` |
| `internal/model` | GORM 实体 | `user.go` |
| `internal/repository` | 数据访问（接口 + GORM 实现） | `user_repo.go`, `db.go` |
| `internal/service` | 业务逻辑（依赖 repo 接口，无 gin） | `user_service.go` |
| `internal/handler` | HTTP 层（DTO、校验、响应） | `user.go`, `health.go` |
| `internal/middleware` | RequestID/Logger/Recovery/CORS/RateLimit/Timeout/Metrics | 同名文件 |
| `internal/errors` | 业务错误码 + 类型化错误 | `errors.go` |
| `internal/response` | 统一响应信封 | `response.go` |
| `internal/router` | 路由表 + 中间件链 + 集成测试 | `router.go`, `integration_test.go` |
| `internal/server` | http.Server + 优雅停机 | `server.go` |
| `internal/testutil` | 测试共享（内存 SQLite） | `db.go` |
| `migrations/` | golang-migrate SQL（版本化） | `000001_create_users_table.*` |
| `docs/` | swag 生成的 OpenAPI（提交入库） | `swagger.json/yaml`, `docs.go` |

## 4. 配置模型

优先级：**内置默认值 < `configs/config.yaml` < 环境变量（`APP_` 前缀，`.`→`_`）**；`.env` 由 godotenv 预载（可选）。

```yaml
app:      {name, env, debug, port: 8080, graceful_timeout}
http:     {read_timeout, write_timeout, idle_timeout, shutdown_timeout}
database: {driver: postgres, host, port, user, password, dbname, sslmode, max_open_conns, max_idle_conns, conn_max_lifetime, log_level}
log:      {level: info|debug|warn|error, format: json|console}
cors:     {allowed_origins, allowed_methods, allowed_headers, exposed_headers, allow_credentials, max_age}
ratelimit:{enabled, rps, burst}
swagger:  {enabled}
```

- 关键环境变量：`APP_PORT`、`APP_DATABASE_HOST/PORT/USER/PASSWORD/DBNAME`、`APP_LOG_LEVEL/FORMAT`、`APP_RATELIMIT_RPS/BURST`、`APP_CORS_ALLOWED_ORIGINS`（逗号分隔）、`APP_SWAGGER_ENABLED`。
- 所有时长键用字符串（`"10s"`）；viper 默认挂 `StringToTimeDuration` 钩子。
- 显式 `-config` 缺失 → 启动失败；默认路径缺失 → 用默认值 + env。
- 生产建议：密钥走环境变量/密钥管理；`APP_DEBUG=false`、CORS 收紧、`APP_LOG_FORMAT=json`。

## 5. 数据模型与迁移

`users` 表（`migrations/000001_create_users_table.*`）：

| 列 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| name | varchar(64) NOT NULL | |
| email | varchar(255) NOT NULL | 部分唯一索引 `WHERE deleted_at IS NULL`（软删下允许复用邮箱） |
| status | smallint NOT NULL DEFAULT 1 | 1=active 0=disabled（`model.UserStatus`） |
| created_at / updated_at | timestamptz NOT NULL | |
| deleted_at | timestamptz NULL | **软删除**：`Delete` 只置该列，行保留审计 |

- 迁移工具：golang-migrate（SQL 文件），`make migrate-up/down/create`。
- **GORM model 与迁移 SQL 是两套事实来源**，改表必须两处同步；测试库用 `AutoMigrate`（`internal/testutil`）自动建表。
- 新表记住：唯一索引要考虑软删除场景；查询保持 `LOWER()` 可移植写法。

## 6. API 契约

统一信封：`{"code": <int>, "message": <string>, "data": <any|omitempty>}`；`code=0` 成功。

业务错误码（`internal/errors`）：`0` 成功；`40000` 参数错误；`40100` 未认证；`40300` 无权限；`40400` 不存在；`40900` 冲突；`42900` 限流；`50000` 内部错误；`50001` 数据库错误。HTTP 状态 = 码/100 就近映射。

| 端点 | 方法 | 说明 | 成功 | 失败 |
|---|---|---|---|---|
| `/healthz` | GET | 存活探针（不依赖外部） | 200 | — |
| `/readyz` | GET | 就绪探针（ping DB） | 200 | 503 |
| `/metrics` | GET | Prometheus 指标 | 200 | — |
| `/swagger/*any` | GET | Swagger UI（`swagger.enabled` 控制） | 200 | — |
| `/api/v1/users` | POST | 建用户 `{name(2-64), email}` | 201 | 400/409 |
| `/api/v1/users` | GET | 列表 `?page&page_size&keyword` | 200 `{items,total,page,page_size}` | — |
| `/api/v1/users/:id` | GET | 详情 | 200 | 400/404 |
| `/api/v1/users/:id` | PATCH | 局部更新（指针字段区分未传） | 200 | 400/404/409 |
| `/api/v1/users/:id` | DELETE | 软删除 | 204 | 400/404 |

中间件对 `/api/v1/*` 生效（含限流、超时 504）；运维端点豁免限流。

## 7. 中间件与可观测性

- 链路：`Recovery → RequestID → Logger → CORS → Metrics → (v1: RateLimit → Timeout)`。
- `X-Request-ID`：入站复用或生成 UUID；响应头回写；日志/错误响应均携带。
- 指标：`http_requests_total{method,path,status}`、`http_request_duration_seconds{method,path}`、`http_requests_in_flight`；path 用路由模式（`/api/v1/users/:id`）控基数。
- 限流：每 IP token bucket（`x/time/rate`），X-Forwarded-For 取第一跳；超限 429 + `Retry-After`。
- 优雅停机：SIGINT/SIGTERM → `signal.NotifyContext` → `http.Server.Shutdown`（`http.shutdown_timeout` 上限）。

## 8. 测试策略

- 单元：`internal/config`（优先级/校验）、`internal/service`（手写 mock repo，验证业务规则）。
- 集成：`internal/router/integration_test.go`（`package router_test`）内存 SQLite 走 路由→handler→service→repo 全链路 CRUD。
- 仓储：`internal/repository` 直接对 SQLite 测（验证软删、唯一冲突、分页）。
- 测试不依赖 Docker/PostgreSQL；sqlite 驱动仅测试可见；每个测试用独立命名内存库，可并行。
- 跑法：`make check`（提交前唯一门槛）；纯 Go 改动最快闭环 `make test`；端到端 `make smoke`（需 postgres）。

## 9. 关键设计决策（ADR 精简版）

1. **Gin + GORM + zap + viper**：生态最大、招聘/维护成本最低，模板面向大多数团队。
2. **接口驱动分层**：service 依赖 repo 接口、handler 依赖 service 接口 → 单测不碰真实 DB，实现可替换。
3. **统一信封 + 业务码**：前端/网关可稳定判断错误；`Error.Is` 按 Code 匹配（见 AGENTS.md）。
4. **软删除**：默认保留数据，唯一索引按 `deleted_at IS NULL` 部分索引保证邮箱可复用。
5. **SQLite 仅测试**：生产只用 PostgreSQL；测试用 `file::memory:?cache=shared` 内存库。
6. **手工 DI**：规模小，wire/fx 引入的复杂度不值当；未来可演进。
7. **promhttp 直挂**：`/metrics` 用 `promhttp.Handler()`，不依赖 DefaultServeMux 自动注册。
8. **迁移与 model 双源**：SQL 迁移管生产 schema，AutoMigrate 只服务测试，避免启动时隐式改库。
9. **前端 TS 6.0 + `ignoreDeprecations: "6.0"`**：`web/admin` 锁 `typescript ^6.0.3`（实测 vue-tsc 2.2.12 兼容，typecheck/build 全绿）。`ignoreDeprecations` 只接受当前 major 对应的值：TS 5.x 仅认 `"5.0"`（写 `"6.0"` 报 TS5103），TS 6.0/7.0 认 `"6.0"`。升级 TS 大版本时需同步调整该值，先改 tsconfig 再升依赖。

## 10. 扩展指南

**新增领域模块**（详见 skill `/add-domain`）：model → 迁移 → repository 接口+实现 → service → handler(DTO+校验+Swagger 注解) → router 注册 → 单测+集成测试 → `make swagger`。

**新增配置项**：`config.go` 加字段 → `loader.go` `setDefaults` 加默认值 → `config.example.yaml`/`config.yaml` 同步 → 必要时加 Validate 分支。

**新增中间件**：写 `internal/middleware/<name>.go`（gin.HandlerFunc）→ `router.go` 挂链（注意顺序：Recovery 最外、RequestID 先于 Logger）。

**新增运维端点**：放 `router.go` 顶层组（豁免限流）；探针逻辑在 `internal/handler/health.go`。

**加认证**：建议在 `RateLimit` 前挂 JWT 中间件，错误码用 `40100/40300`。

## 11. 已知限制

- 限流是单实例内存态：多副本部署需换 Redis 计数（`x/time/rate` 不支持分布式）。
- `Timeout` 中间件基于 goroutine 包裹，超时后处理协程仍会跑完（无法写回客户端）——设计取舍。
- 测试库为每测试独立的命名内存 SQLite（`internal/testutil`），可并行；但仍需避免依赖真实 PostgreSQL 特性。

> 报错速查、环境陷阱、linter/前端/容器排错：见 **[docs/troubleshooting.md](docs/troubleshooting.md)**（易变内容不写在本文件）。
