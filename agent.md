# agent.md — 本仓库的 AI 协作操作手册

> 给任何在本仓库工作的 AI agent（或人类协作者）的操作规范。改动代码前先读本文件与 `context.md`；
> 需要可复用操作配方时，加载 `skills/` 下对应 skill（如 `/add-domain`、`/run-checks`、`/debug-e2e`、`/docker-deploy`、`/database-migrations`）。

## 项目一句话

Go Web 单体应用模板：**Gin + GORM + PostgreSQL + uber-go/zap**，分层架构 `handler → service → repository`，统一响应信封 + 业务错误码，开箱即用的 Docker / CI / Swagger / Prometheus。

## 常用命令（等价 Makefile 目标）

```bash
make build          # 编译到 bin/
make run            # 直接运行（需 postgres）
make dev            # air 热重载
make test           # 全部单测（go test ./...）
make test-race      # 竞态检测
make coverage       # 覆盖率
make vet            # go vet
make lint           # golangci-lint
make fmt            # gofmt + goimports
make swagger        # 重新生成 docs/（改了注解后必须跑）
make migrate-up / migrate-down / migrate-create NAME=xxx
make docker-up / docker-down / docker-build / docker-logs
```

## 硬性规则（违反 = 打回）

1. **分层依赖方向唯一**：`handler → service → repository`。禁止反向 import；`internal/` 任何包不得被 `internal/` 外的非 main 包 import；service 层禁止 import gin。
2. **错误处理**：业务错误一律用 `internal/errors`（`apperrors.Wrap/New` + 稳定 Code）；handler 层用 `response.Fail(c, err)` 输出统一信封 `{code, message, data}`。不要返回裸 error 或自造 JSON。
3. **校验**：请求 DTO 用 gin binding tag（`binding:"required,email"`），handler 内 `ShouldBindJSON` 失败 → `apperrors.CodeInvalidArgument`。
4. **日志**：只用 zap（通过 `app.NewLogger` 构建）；访问日志由中间件产出，业务代码不要 print。
5. **上下文**：所有 service/repository 方法首参必须是 `context.Context`，透传 `c.Request.Context()`；DB 查询一律 `WithContext(ctx)`。
6. **测试**：新增领域必须带测试——service 层手写 mock repo 单测 + router 包集成测试（`internal/router/integration_test.go`，内存 SQLite 走全链路）。测试禁止依赖真实 PostgreSQL。
7. **质量门槛**：每次改动后 `make vet && make lint && make test` 必须全绿，`gofmt -l .` 为空。改了 Swagger 注解必须 `make swagger` 并把 `docs/` 一起提交。
8. **依赖**：新增依赖后 `go mod tidy`；sqlite 驱动只允许出现在测试路径（`internal/testutil`），不得进入生产二进制。

## 本机环境已知坑（实测，务必遵守）

- **curl 被系统代理劫持**：本机 shell 有 `all_proxy=http://127.0.0.1:7890`，访问本地服务必须加 `--noproxy '*'`，否则 curl 挂起超时。
- **长驻进程**：在 bash 工具里 `&` 起服务器会被等待并超时 SIGTERM 杀掉；服务器类长驻进程要用**受管后台任务**（`run_in_background: true`）运行，验证完再 kill。
- **macOS 下 migrate 容器访问 postgres**：compose 网络内用服务名 `postgres`；裸 `docker run` 用 `host.docker.internal`（或 `--network host` + `127.0.0.1`）。
- **compose 相对路径**：`deploy/docker-compose.yml` 的相对路径（如 `../migrations`）相对 **compose 文件所在目录**解析，不是项目根。
- **配置失败语义**：显式 `-config path` 指向不存在的文件 = fail-fast；不传 `-config` 且默认 `configs/config.yaml` 不存在 = 回退默认值 + 环境变量。
- **YAML 时长必须是字符串**：`"10s"`、`"1h"`；写成裸数字会被当纳秒。env 变量 `APP_*` 同理（viper 自动转 duration）。
- **GORM 查询可移植性**：SQL 里不要用 PostgreSQL 专属 `ILIKE`（SQLite 测试会挂），用 `LOWER(col) LIKE ?`。
- **import 循环**：handler 包测试不要 import router（router import handler）；集成测试放在 router 包内用 `package router_test`。
- **业务错误码的 errors.Is**：`apperrors.Error.Is` 按 Code 匹配，所以 `errors.Is(err, apperrors.ErrNotFound)` 对 Wrap 出来的错误同样成立——测试里放心用。
- **metrics**：`/metrics` 用 `promhttp.Handler()` 直挂；新版 client_golang 不再自动注册到 DefaultServeMux。
- **改模块路径**：用 `./scripts/rename-module.sh github.com/你的组织/项目`（会同步 go.mod、import、golangci local-prefixes）。

## 标准改动流程（checklist）

1. 读 `context.md` 对应章节 + 加载相关 skill（新增领域 → `/add-domain`）。
2. 写代码：遵循分层与命名规范（见 context.md「编码约定」）。
3. 写/更新测试；跑 `make vet && make lint && make test`。
4. 涉及 API 变更 → 补 Swagger 注解 → `make swagger` → 提交 `docs/`。
5. 涉及表结构 → `make migrate-create NAME=xxx` 写 up/down SQL，并同步 GORM model（若同时改了 model 的 AutoMigrate 场景注意测试库）。
6. 同步更新 `context.md`（架构/API/配置变化）与 `README.md`（用户可见变化）。
7. `git add -A && git commit`（语义化 message，中文描述也可）。

## 何时用哪个 skill

| 任务 | skill |
|---|---|
| 新增一个领域模块（model→迁移→repo→service→handler→路由→测试） | `/add-domain` |
| 提交前全量质量检查 | `/run-checks` |
| 本地起全栈、调 API、排查问题 | `/debug-e2e` |
| 镜像构建 / compose 编排 / CI | `/docker-deploy` |
| 数据库迁移增改查 | `/database-migrations` |
