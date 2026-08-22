# go-web-template

行业标准的 Go Web 单体应用模板（Monolith），开箱即用。

> 技术栈：**Go 1.26 · Gin · GORM · PostgreSQL · uber-go/zap · viper · golang-migrate · Prometheus · Swagger**

## 特性

- **分层架构**：`handler → service → repository`，接口驱动、可独立测试
- **统一响应格式**：`{ code, message, data }` + 稳定的业务错误码（`internal/errors`）
- **配置管理**：默认值 < `configs/config.yaml` < 环境变量（`APP_*` 前缀），支持 `.env`
- **结构化日志**：zap JSON 日志，访问日志携带 request-id、耗时、状态码
- **可观测性**：`/healthz` 存活探针、`/readyz` 就绪探针（含 DB 检查）、`/metrics` Prometheus 指标
- **健壮性**：panic 恢复、请求超时中间件、每 IP 限流（token bucket）、CORS、优雅停机
- **数据库**：GORM + PostgreSQL，连接池参数化、慢查询日志；`golang-migrate` 版本化 SQL 迁移（软删除示例）
- **文档**：Swagger 注解自动生成 OpenAPI 文档，`/swagger/index.html` 在线预览
- **前端**：`web/admin/` 配套 Admin 前端模板（Vue 3 + TS + Element Plus + UnoCSS），登录 / 仪表盘 / 用户 CRUD 开箱即用
- **工程化**：Makefile、Docker 多阶段构建、docker-compose（DB+迁移+应用）、GitHub Actions CI、golangci-lint、单元测试全绿

## 快速开始

前置条件：Go ≥ 1.26、Docker。

```bash
# 1. 一键起 postgres + 迁移 + 应用（或分步执行下方命令）
make docker-up

# 2. 验证
curl http://localhost:8080/healthz        # {"status":"ok"}
curl http://localhost:8080/readyz         # {"status":"ok"}
curl http://localhost:8080/swagger/index.html
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
# 创建用户
curl -X POST http://localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice","email":"alice@example.com"}'
# => 201 {"code":0,"message":"created","data":{"id":1,"name":"Alice","email":"alice@example.com","status":1,...}}

# 列表（分页 + 关键字）
curl 'http://localhost:8080/api/v1/users?page=1&page_size=20&keyword=ali'

# 详情 / 更新 / 删除
curl http://localhost:8080/api/v1/users/1
curl -X PATCH http://localhost:8080/api/v1/users/1 -H 'Content-Type: application/json' -d '{"name":"Alicia"}'
curl -X DELETE http://localhost:8080/api/v1/users/1
```

完整接口定义见 Swagger UI（`/swagger/index.html`）。

## 项目结构

```
go-web-template/
├── cmd/server/            # 进程入口（main），只做装配
├── internal/
│   ├── app/               # 组件装配与生命周期（Run、logger 构建）
│   ├── config/            # 配置模型 + viper 加载（文件/环境变量）
│   ├── model/             # GORM 领域模型
│   ├── repository/        # 数据访问层（GORM，接口 + 实现）
│   ├── service/           # 业务逻辑层（依赖 repository 接口）
│   ├── handler/           # HTTP 层（参数校验、响应渲染）
│   ├── middleware/        # 中间件：RequestID/Logger/Recovery/CORS/限流/超时/Metrics
│   ├── response/          # 统一响应信封
│   ├── errors/            # 业务错误码与类型化错误
│   ├── router/            # 路由表 + 中间件链装配（含集成测试）
│   ├── server/            # http.Server 与优雅停机
│   └── testutil/          # 测试共享工具（内存 SQLite）
├── migrations/            # golang-migrate SQL 迁移
├── configs/               # YAML 配置（config.yaml / config.example.yaml）
├── deploy/                # docker-compose.yml
├── docs/                  # 由 swag 生成的 OpenAPI 文档
├── scripts/               # dev.sh（一键开发）、rename-module.sh（改模块路径）
├── web/admin/             # Admin 前端模板（Vue 3 + TS + Vite + Element Plus + UnoCSS）
├── .github/workflows/     # CI：lint + vet + test -race + build + docker
├── Dockerfile             # 多阶段构建（~10MB 运行时镜像）
├── Makefile               # build/run/dev/test/lint/swagger/migrate/docker 等
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
| `APP_CORS_ALLOWED_ORIGINS` | 允许的跨域来源（逗号分隔） | `*` |
| `APP_SWAGGER_ENABLED` | 是否暴露 Swagger UI | `true` |

生产环境建议：通过密钥管理注入 `APP_DATABASE_PASSWORD` 等敏感项；将 CORS 收紧为实际域名；`APP_DEBUG=false`、`APP_LOG_FORMAT=json`。

## 常用命令

```bash
make build          # 编译到 bin/
make run            # 直接运行
make dev            # air 热重载
make test           # 单元测试
make test-race      # 竞态检测
make coverage       # 覆盖率报告
make lint           # golangci-lint
make swagger        # 重新生成 OpenAPI 文档
make migrate-up     # 执行迁移
make migrate-create NAME=add_posts_table   # 新建迁移
make docker-up      # 一键：postgres + migrate + app
make docker-down    # 停止
```

## 测试

```bash
make test
```

测试策略：

- **单元测试**：config（文件/环境变量优先级）、service（手写 mock repository）
- **集成测试**：`internal/router/integration_test.go` 用内存 SQLite 走通 路由 → handler → service → repository 全链路 CRUD
- 测试运行无需 PostgreSQL（SQLite 仅用于测试，不进生产镜像）

## CI / CD

`.github/workflows/ci.yml`：push / PR 触发 → `golangci-lint` → `go vet` → `go test -race` → `go build` → Docker 镜像构建。

## 从模板开始你的项目

1. 重命名模块路径（影响 go.mod 与所有 import）：
   ```bash
   ./scripts/rename-module.sh github.com/yourorg/your-project
   ```
2. 按需新增领域：在 `model/` 加实体 → `migrations/` 加迁移 → `repository/` 写仓储 → `service/` 写业务 → `handler/` 写接口 → `router/` 注册路由（含 Swagger 注解）。
3. 常用扩展方向：JWT 认证中间件、Redis 缓存、消息队列（Kafka/RabbitMQ）、gRPC 网关、多租户。

## AI 协作文件

本仓库为 AI 代理（及人类协作者）准备了上下文与可复用配方，确保后续改动可靠、风格一致：

- **[AGENTS.md](AGENTS.md)** — 操作手册（AI 代理与人类协作者通用）：硬性规则（分层依赖方向、错误处理、测试要求）、常用命令、**本机环境已知坑**（curl 代理、后台进程、compose 路径等）、标准变更流程、skill 索引。
- **[context.md](context.md)** — 项目上下文：架构总览、包职责、配置模型、数据模型、API 契约、设计决策（ADR）、扩展指南。
- **[skills/](skills/)** — 可复用操作配方（同时以符号链接暴露于 `.agents/skills/` 供自动发现）：

| skill | 用途 |
|---|---|
| `add-domain` | 新增领域模块（model→迁移→repo→service→handler→路由→测试→Swagger 八步） |
| `run-checks` | 提交前质量检查（fmt/vet/lint/test/race/coverage） |
| `debug-e2e` | 本地端到端调试与排查 |
| `docker-deploy` | 镜像/compose/CI 构建部署 |
| `database-migrations` | 迁移生命周期管理 |

## License

MIT
