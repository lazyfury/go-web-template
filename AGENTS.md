# AGENTS.md — AI 协作约定与操作手册

> 适用于任何在本仓库工作的 agent（也适用于人类协作者）。
> **从仓库根启动会话**：项目技能通过 `.agents/skills/` 暴露，会话目录不在仓库根时不会被发现。

## 事实来源分工（避免同一规则写三遍）

| 文件 | 只放什么 |
|---|---|
| `AGENTS.md`（本文件） | **规则、流程、完成定义**；不抄命令、不抄操作细节 |
| `context.md` | **架构、数据模型、API 契约、配置、ADR**（append-only，不写排错） |
| `skills/` | **可执行配方**（命令 + 断言），按任务加载 |
| `docs/troubleshooting.md` | **踩坑 → 处理**（报错速查、环境陷阱） |
| `Makefile` | **命令的唯一权威**（看 `make help`；README/AGENTS 不复写） |
| `.agents/local.md` | 本机环境差异（gitignored；存在则先读） |

## 项目一句话

Go Web 单体应用模板：**Gin + GORM + PostgreSQL + zap**，分层 `handler → service → repository`，统一响应信封 + 业务错误码，内置 Docker / CI / Swagger / Prometheus。

## 开工顺序

1. 读 `context.md` 的相关章节（§2 架构、§3 包职责、§5 数据模型、§6 API 契约）。
2. 任务匹配技能就加载对应 `skills/<name>/SKILL.md`（索引见文末）。
3. `make help` 查命令；工具缺失先 `make tools`。

## 硬性规则（违反 = 打回）

1. **分层与依赖方向**：业务包内 `handler → service → repository`（同一包内按文件分层）；包级方向 `cmd → app → {web, user, platform/*}`，`platform/*` 是叶子包。禁止反向 import；service 不得 import gin；`web` 不得 import 业务域（域用 `RegisterRoutes` 自己挂载）；`internal/` 不得被外部非 main 包 import。
2. **错误处理**：业务错误一律 `internal/platform/apperr`（`apperr.Wrap/New` + 稳定 Code）；handler 用 `httpx.Fail(c, err)` 输出 `{code,message,data}`，不返回裸 error、不自造 JSON。
3. **校验**：请求 DTO 用 gin binding tag；`ShouldBindJSON` 失败 → `apperr.CodeInvalidArgument`。
4. **日志**：只用 zap（`logx.New` 构建）；访问日志由中间件产出，业务代码不 print。
5. **上下文**：service/repository 方法首参必须是 `context.Context`，透传 `c.Request.Context()`；DB 查询 `WithContext(ctx)`。
6. **测试**：新领域必须带 service 层 mock repo 单测 + `internal/app` 全链路集成测试（`app.NewWithDB`）；测试不得依赖真实 PostgreSQL。
7. **依赖**：新增依赖后 `go mod tidy`；sqlite 只允许出现在测试路径（`internal/testutil`）。
8. **可移植 SQL**：不用 PostgreSQL 专属语法（如 `ILIKE`），测试库是 SQLite。

## 质量门（唯一入口）

```bash
make check      # = golangci-lint + gofmt 校验 + vet + race 测试 + build + docs/依赖 同步
```

- CI 跑同一套（`make gate` + golangci-lint job + web typecheck/build + docker build）。
- 工具缺失 → `make tools`（版本 pin 在 `Makefile` 顶部，**不要 `@latest`**）。
- 改了 Swagger 注解 → `make swagger` 并提交 `docs/`（`make check` 会检出漂移）。
- 只改 Go 代码时，最快闭环是 `make test`：测试用内存 SQLite，**不需要 postgres/docker**。
- **禁止未运行就声称通过**：结论要有真实输出支撑。

## 标准改动流程

1. 定位：读 `context.md` 相关章节 + 加载相关 skill。
2. 改代码：遵循分层与命名（`context.md` §3/§10）；新增领域走 `add-domain` skill。
3. 补/改测试。
4. `make check` 全绿。
5. 同步文档：架构/契约/配置变化 → `context.md`；新踩的坑 → `docs/troubleshooting.md`；用户可见 → `README.md`。
6. 提交：先 `git status --short` 复核，再**按文件路径** `git add <paths>`（**不要 `git add -A`**），语义化 message。

## 必须先问人（破坏性 / 不可逆）

- `git push --force`、改写已推送历史、删除或重写**已应用**的迁移文件。
- `make migrate-down` 作用在共享/预发/生产库。
- `docker compose down -v`、删除数据卷、`rm -rf` 非构建产物。
- 提交密钥/`.env`、修改 CI 触发条件或仓库权限。
- 改动超出任务范围的重构（先确认再动）。

## 完成定义（DoD）

最终回复必须包含：

1. **变更文件清单**（按层：model / repository / service / handler / router / docs）。
2. **`make check` 的真实输出摘要**（贴最后几行，不要只写"已通过"）。
3. **未验证项与风险**（例如"本机无 docker，未跑 compose e2e；已用 `./scripts/smoke.sh` 验证 liveness 路径"）。

## 技能索引

| skill | 用途 |
|---|---|
| `add-domain` | 新增领域模块（model→迁移→repo→service→handler→路由→测试→Swagger） |
| `docker-ops` | 容器编排 + 本地端到端调试（也可用 `make smoke` 一条命令覆盖） |
| `database-migrations` | 迁移新建/执行/回滚 + GORM model 同步 checklist |
