# context.md — 项目上下文

> 本文件是仓库的「活文档」：架构、数据流、配置、API 契约、设计决策与扩展指南。
> agent 改动代码前先读本文件；改动影响架构/API/配置时，记得同步更新本文件。

## 1. 项目定位

- 行业标准 Go Web **单体应用**模板（HTTP JSON API 服务），非微服务。
- 技术栈：Go 1.26 / Gin / GORM / PostgreSQL / uber-go/zap / viper / golang-migrate / Prometheus / OpenAPI 3.0（routekit 代码生成）。
- 模块路径：`github.com/example/go-web-template`（真实项目用 `scripts/rename-module.sh` 替换）。
- **配套前端**：`web/admin/`（Vue 3 + TS + Vite + Element Plus + UnoCSS），登录（mock 可切换真实接口）/ 仪表盘 / users CRUD；开发时 vite 将 `/api` 代理到 `:8080`。API 类型由 `docs/openapi.json` 生成（`pnpm gen:api`），详见 `web/admin/README.md`。

## 2. 架构总览

```
                       ┌──────────────────────────── HTTP :8080 ─────────────────────────────┐
                       │                                                                     │
  cmd/server/main ───► │  internal/web   (engine + 中间件链 + 探针 + 优雅停机)                │
                       │    │ Recovery → RequestID → Logger → CORS → Metrics                 │
                       │    │ /api/v1 组: RateLimit → Timeout(ctx deadline)                  │
                       │    ▼                                                               │
                       │  internal/user   (业务一块：handler → service → repository 同包分文件) │
                       │    ▼                                                               │
                       │  internal/platform/{config,logx,dbx,apperr,httpx} 叶子包 ─► PostgreSQL
                       │  internal/platform/routekit（路由元数据 → OpenAPI）              │
                       │                                                                     │
                       │  运维端点: /healthz /readyz /metrics /openapi.json /swagger/*any   │
                       │  （不经限流；OpenAPI 由 routekit 路由元数据在启动时生成）           │
                       └─────────────────────────────────────────────────────────────────────┘
```

- **入口**：`cmd/server/main.go` 只做 flag 解析 → `config.Load` → `logx.New` → `app.New` → `App.Run`（`defer App.Close`）。
- **装配（组合根）**：`internal/app` 手工依赖注入（DB→repo→service→handler→web→server）。`NewWithDB(cfg, logger, db)` 是测试缝：集成测试用同一套生产装配跑内存 SQLite。
- **请求生命周期**：中间件链 → handler 校验 DTO → service 业务规则（依赖 repo 接口）→ repository 执行 SQL → 统一 `httpx.Body` 输出。
- **依赖方向**（禁止反向）：`cmd → app → {web, user, platform/*}`；`user → platform/*`；`web → platform/*`。`platform/*` 是叶子包；`web` 不得 import 业务域（域通过 `RegisterRoutes` 自己挂载）。`routekit` 只依赖 `httpx/apperr/gin`，不依赖任何业务域。

## 3. 包职责速查

| 包 | 职责 | 关键文件 |
|---|---|---|
| `cmd/server` | 进程入口 | `main.go`（version/commit ldflags） |
| `internal/app` | 组合根：装配 + 生命周期 + 集成测试 | `app.go`, `integration_test.go` |
| `internal/platform/apperr` | 业务错误码 + 类型化错误（纯） | `apperr.go` |
| `internal/platform/httpx` | 统一响应信封 OK/Created/NoContent/Fail | `response.go` |
| `internal/platform/config` | 配置模型 + viper 加载 | `config.go`, `loader.go` |
| `internal/platform/logx` | zap 构建（只依赖字符串参数） | `logger.go` |
| `internal/platform/dbx` | GORM PostgreSQL 连接池 | `db.go` |
| `internal/platform/routekit` | 声明式路由元数据 + 反射生成 OpenAPI 3.0.3 | `types.go`, `schema.go`, `registry.go`, `group.go`, `serve.go` |
| `internal/web` | 紧凑 HTTP 层：engine/中间件/探针/优雅停机/文档挂载（无业务） | `router.go`, `docs.go`, `middleware_*.go`, `health.go`, `server.go` |
| `internal/user` | 业务一块：entity/repo/service/handler/dto/routes 同包分文件 | `entity.go`, `repository.go`, `service.go`, `handler.go`, `dto.go`, `routes.go` |
| `internal/testutil` | 测试共享（内存 SQLite，显式传 models） | `db.go` |
| `migrations/` | golang-migrate SQL（版本化） | `000001_create_users_table.*` |
| `docs/` | 生成的 OpenAPI 契约 + 手写排错文档 | `openapi.json`, `troubleshooting.md` |

## 4. 配置模型

优先级：**内置默认值 < `configs/config.yaml` < 环境变量（`APP_` 前缀，`.`→`_`）**；`.env` 由 godotenv 预载（可选）。

```yaml
app:      {name, env, debug, port: 8080}
http:     {read_timeout, write_timeout, idle_timeout, shutdown_timeout, request_timeout, max_body_bytes}
database: {driver: postgres, host, port, user, password, dbname, sslmode, max_open_conns, max_idle_conns, conn_max_lifetime, log_level}
log:      {level: info|debug|warn|error, format: json|console}
cors:     {allowed_origins, allowed_methods, allowed_headers, exposed_headers, allow_credentials, max_age}
ratelimit:{enabled, rps, burst}
swagger:  {enabled}   # /openapi.json + Swagger UI
```

- 关键环境变量：`APP_PORT`、`APP_DATABASE_HOST/PORT/USER/PASSWORD/DBNAME`、`APP_LOG_LEVEL/FORMAT`、`APP_RATELIMIT_RPS/BURST`、`APP_HTTP_REQUEST_TIMEOUT`、`APP_HTTP_MAX_BODY_BYTES`、`APP_CORS_ALLOWED_ORIGINS`（逗号分隔）、`APP_SWAGGER_ENABLED`。
- 所有时长键用字符串（`"10s"`）；viper 默认挂 `StringToTimeDuration` 钩子。
- 显式 `-config` 缺失 → 启动失败；默认路径缺失 → 用默认值 + env。
- 生产建议：密钥走环境变量/密钥管理；`APP_DEBUG=false`、CORS 收紧、`APP_LOG_FORMAT=json`。
- `deploy/docker-compose.yml` 是生产式编排，默认 `APP_SWAGGER_ENABLED=false`；本地看文档用 `APP_SWAGGER_ENABLED=true make docker-up`。

## 5. 数据模型与迁移

`users` 表（`migrations/000001_create_users_table.*`）：

| 列 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| name | varchar(64) NOT NULL | |
| email | varchar(255) NOT NULL | 部分唯一索引 `WHERE deleted_at IS NULL`（软删下允许复用邮箱） |
| status | smallint NOT NULL DEFAULT 1 | 1=active 0=disabled（`user.Status`） |
| created_at / updated_at | timestamptz NOT NULL | |
| deleted_at | timestamptz NULL | **软删除**：`Delete` 只置该列，行保留审计 |

- 迁移工具：golang-migrate（SQL 文件），`make migrate-up/down/create`。
- **GORM model 与迁移 SQL 是两套事实来源**，改表必须两处同步；测试库用 `AutoMigrate`（`internal/testutil.NewSQLiteDB(t, &user.User{})`）自动建表。
- 新表记住：唯一索引要考虑软删除场景；查询保持 `LOWER()` 可移植写法。
- HTTP 响应使用 `internal/user/dto.go` 的 `UserDTO`/`ListResponse`，GORM entity 不直接作为 API 模型；暴露新字段必须改 DTO，避免 DB 字段意外泄漏。

## 6. API 契约

统一信封：`{"code": <int>, "message": <string>, "data": <any|omitempty>}`；`code=0` 成功。

业务错误码（`internal/platform/apperr`）：`0` 成功；`40000` 参数错误；`40100` 未认证；`40300` 无权限；`40400` 不存在；`40900` 冲突；`41300` 请求体过大；`42900` 限流；`50000` 内部错误；`50001` 数据库错误。已知码精确映射到 HTTP 状态，未知码按 `[HTTP][类别][序号]` 布局回退。

错误响应额外带 `request_id`（与 `X-Request-ID` 一致，便于客户端关联日志）。OpenAPI 文档由 `routekit.Op` 元数据生成，响应的 `data` 使用真实 DTO 类型（不是 `any`）。

| 端点 | 方法 | 说明 | 成功 | 失败 |
|---|---|---|---|---|
| `/healthz` | GET | 存活探针（不依赖外部） | 200 | — |
| `/readyz` | GET | 就绪探针（ping DB） | 200 | 503 |
| `/metrics` | GET | Prometheus 指标 | 200 | — |
| `/openapi.json` | GET | OpenAPI 3.0 文档（`swagger.enabled` 控制） | 200 | — |
| `/swagger/*any` | GET | Swagger UI，读取 `/openapi.json`（`swagger.enabled` 控制） | 200 | — |
| `/api/v1/users` | POST | 建用户 `{name(2-64), email}` | 201 | 400/409/413 |
| `/api/v1/users` | GET | 列表 `?page&page_size&keyword` | 200 `{items,total,page,page_size}` | — |
| `/api/v1/users/:id` | GET | 详情 | 200 | 400/404 |
| `/api/v1/users/:id` | PATCH | 局部更新（指针字段区分未传） | 200 | 400/404/409 |
| `/api/v1/users/:id` | DELETE | 软删除 | 204 | 400/404 |

中间件对 `/api/v1/*` 生效：先 `RateLimit`，再 `Timeout` 向 `c.Request.Context()` 注入 `http.request_timeout` deadline（service/repository 通过 ctx 传递；handler 不检查 ctx 时不会自动中断，硬超时由 http server 的 read/write timeout 兜底）。运维端点豁免限流。

## 7. 中间件与可观测性

- 链路：`Recovery → RequestID → Logger → CORS → Metrics → BodyLimit → (v1: RateLimit → Timeout)`。`BodyLimit` 按 `http.max_body_bytes`（默认 1 MiB）拒绝过大请求体。
- `X-Request-ID`：入站复用或生成 UUID；响应头回写；访问日志与错误响应体（`request_id` 字段）均携带；错误响应统一经 `httpx.Fail`/`httpx.Error` 生成。
- 指标：`http_requests_total{method,path,status}`、`http_request_duration_seconds{method,path}`、`http_requests_in_flight`；path 用路由模式（`/api/v1/users/:id`），未匹配路由固定记为 `unmatched`（避免扫描器打爆基数）。
- 限流：每 IP token bucket（`x/time/rate`），X-Forwarded-For 取第一跳；超限 429 + `Retry-After`。
- 超时：`Timeout` 只注入 context deadline，不另起 goroutine、不伪造响应（`gin.Context` 非并发安全，goroutine 版会 data race）。需要硬拒绝时在 handler 内检查 `ctx.Err()` 并返回业务错误。
- 优雅停机：SIGINT/SIGTERM → `signal.NotifyContext` → `http.Server.Shutdown`（`http.shutdown_timeout` 上限）。

## 8. 测试策略

- 独立包单测（不依赖 gin/gorm）：`platform/apperr`（码表/`From`/`Unwrap`）、`platform/httpx`（信封与错误映射）、`platform/logx`、`platform/config`（优先级/校验）。
- 业务单测：`internal/user/service_test.go`（手写 fake `Repository`，只测规则；`NewService` 返回具体类型，接口只在消费侧）。
- 仓储：`internal/user/repository_test.go` 直接对 SQLite 测（软删、唯一冲突、分页）。
- 传输层：`internal/web/*_test.go`（RequestID/CORS/RateLimit/Timeout/Recovery/Metrics/Health，`httptest` 驱动）。
- 集成：`internal/app/integration_test.go`（`package app_test`）用 `app.NewWithDB` + 内存 SQLite 走**生产同款装配**全链路 CRUD。
- OpenAPI：`internal/platform/routekit/*_test.go`（schema/tag/信封/路径参数/确定性）+ `internal/app/openapi_test.go`（黄金文件 `docs/openapi.json`、`engine.Routes()` 完整性、禁用时 404）。
- 前端：`web/admin` 用 `pnpm api:check` 校验生成的 API 类型未漂移，`pnpm build` 做 vue-tsc + vite 构建。
- 测试不依赖 Docker/PostgreSQL；sqlite 驱动仅测试可见；每个测试用独立命名内存库，可并行。
- 跑法：`make check`（提交前唯一门槛）；纯 Go 改动最快闭环 `make test`；端到端 `make smoke`（需 postgres）。

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
11. **前端 TS 6.0 + `ignoreDeprecations: "6.0"`**：`web/admin` 锁 `typescript ^6.0.3`（实测 vue-tsc 2.2.12 兼容，typecheck/build 全绿）。`ignoreDeprecations` 只接受当前 major 对应的值：TS 5.x 仅认 `"5.0"`（写 `"6.0"` 报 TS5103），TS 6.0/7.0 认 `"6.0"`。升级 TS 大版本时需同步调整该值，先改 tsconfig 再升依赖。

## 10. 扩展指南

**新增领域模块**（详见 skill `/add-domain`）：迁移 → `internal/<domain>/entity.go` → `repository.go`（接口+GORM 实现）→ `service.go` → `handler.go`（DTO+校验）→ `routes.go`（`RegisterRoutes(*routekit.Group, *Handler)` + `routekit.Op`）→ `app.NewWithDB` 装配 → 单测 + 集成测试 → `make openapi` → 前端 `cd web/admin && pnpm gen:api`。新增端点必须走 `routekit.Group`，不要直接挂 `*gin.RouterGroup`。

**新增配置项**：`config.go` 加字段 → `loader.go` `setDefaults` 加默认值 → `config.example.yaml`/`config.yaml` 同步 → 必要时加 Validate 分支。

**新增中间件**：写 `internal/web/middleware_<name>.go`（`package web`，gin.HandlerFunc）→ `internal/web/router.go` 挂链（顺序：Recovery 最外、RequestID 先于 Logger）+ 补 `httptest` 单测。

**新增运维端点**：放 `internal/web/router.go` 顶层组（豁免限流）；探针逻辑在 `internal/web/health.go`。

**加认证**：建议在 `RateLimit` 前挂 JWT 中间件，错误码用 `40100/40300`。

## 11. 已知限制

- 限流是单实例内存态：多副本部署需换 Redis 计数（`x/time/rate` 不支持分布式）。
- `Timeout` 只注入 ctx deadline：handler 若不检查 ctx，响应仍会返回成功；需要保证中断的路径必须显式处理 `ctx.Err()`。
- 测试库为每测试独立的命名内存 SQLite（`internal/testutil`），可并行；但仍需避免依赖真实 PostgreSQL 特性。
- OpenAPI 反射只覆盖明确子集：query 数组/对象参数、`*any` 通配路径、自定义 `MarshalJSON`、复杂 `oneof` 尚未支持；不支持的类型会让启动失败，需调整 DTO 或后续扩展显式 schema 覆盖。
- `docs/openapi.json` 由 `make openapi` 更新；手工修改会在 `make check` 的黄金测试中失败。

> 报错速查、环境陷阱、linter/前端/容器排错：见 **[docs/troubleshooting.md](docs/troubleshooting.md)**（易变内容不写在本文件）。
