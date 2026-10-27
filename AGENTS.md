# AGENTS.md — Agent 入口

> 从仓库根启动会话（否则 `.agents/skills/` 不会被发现）。
> 本文件只放：**强约束、工作流、文件导航、渐进式披露**。细节按需加载，不要一次读完。

## 0. 文件导航（事实来源 + 何时读）

| 文件 / 目录 | 只放什么 | 何时读 |
|---|---|---|
| `.agents/local.md` | 本机环境差异（gitignored） | 每次会话先看是否存在 |
| `context.md` | 架构、包职责、数据模型、API 契约、配置、ADR | 开工时读任务相关章节 |
| `docs/agents/README.md` | agent 分册索引 | 需要 workflow/验证/子 agent 细节时 |
| `docs/agents/workflow.md` | 开工、标准改动、文档同步、提交拆分、推送 | 开始任务 / 准备提交 |
| `docs/agents/verification.md` | 三层质量门、命令、证据要求 | 每次提交前、发布前 |
| `docs/agents/subagents.md` | 子 agent 触发点、只读约束、输出格式、成本 | 考虑拆分独立审查 |
| `docs/agents/destructive-operations.md` | 必须先问人的操作、备份/回滚、安全替代 | 动迁移/数据/历史/密钥前 |
| `docs/agents/commit-and-dod.md` | commit 规范、DoD 模板、最终回复格式 | 准备提交 / 写最终回复 |
| `docs/troubleshooting.md` | 报错 → 处理（环境、迁移、lint、前端、容器） | 遇到报错 |
| `docs/design-*.md` | 单模块设计与分期计划 | 设计/实现对应模块 |
| `docs/security-review-2026-10.md` | 黑盒演练记录：发现、复现命令、修复 commit、残留风险 | 改 auth/限流/部署配置、发布前复核 |
| `skills/<name>/SKILL.md` | 可执行配方（命令 + 断言）；`.agents/skills/<name>` 是相对 symlink | 任务匹配时加载 1 个 |
| `Makefile` | **命令唯一权威** | `make help` |
| `docs/openapi.json` | 生成的 OpenAPI 黄金文件 | 不要手改；用 `make openapi` |
| `scripts/` | smoke / verify-auth / audit 脚本 | 质量门需要真实进程时 |

渐进式披露的默认路径：

1. 每次会话：`AGENTS.md` → `.agents/local.md`（存在时）→ `git status --short`。
2. 开工：`context.md` 相关章节 + **一个**匹配的 skill。
3. 提交前：`docs/agents/verification.md` → `docs/agents/commit-and-dod.md`。
4. 表里其他文件只在对应场景加载；不要预读所有 skill/design doc。

## 1. 项目一句话

Go Web 单体模板：**Gin + GORM + PostgreSQL + zap**，分层 `handler → service → repository`，统一响应信封 + 业务错误码，JWT + RBAC，内置 Docker / routekit OpenAPI / Prometheus；业务域含 user/auth/dict/message。

## 2. 硬约束（违反 = 打回）

1. **分层与依赖方向**：业务包内 `handler → service → repository`；包级 `cmd → app → {web, auth, user, dict, message, platform/*}`，`platform/*` 是叶子；`auth → user` 仅单向（`UserStore` 接口）。禁止反向 import；service 不 import gin；`web` 不 import 业务域；`internal/` 不得被外部非 main 包 import。
2. **错误处理**：业务错误一律 `internal/platform/apperr`（`Wrap/New` + 稳定 Code）；handler 用 `httpx.Fail(c, err)` 输出 `{code,message,data}`，不返回裸 error、不自造 JSON。
3. **校验**：请求 DTO 用 gin binding tag；`ShouldBindJSON` 失败 → `apperr.CodeInvalidArgument`。
4. **日志**：只用 zap（`logx.New`）；访问日志由中间件产出；业务代码不 print，不记录密码/token/密钥。
5. **上下文**：service/repository 首参必须是 `context.Context`，透传 `c.Request.Context()`；DB 查询 `WithContext(ctx)`。
6. **测试**：新领域必须有 service 层 mock repo 单测 + `internal/app` 全链路集成测试（`app.NewWithDB`）；测试不得依赖真实 PostgreSQL；受保护端点覆盖匿名 401、越权 403、正常路径。auth/会话行为改动必须同步 `scripts/verify-auth.sh` 断言（真实进程 + 真实 PG）。
7. **依赖**：新增依赖后 `go mod tidy`；sqlite 只允许出现在测试路径（`internal/testutil`）。
8. **可移植 SQL**：不用 PostgreSQL 专属语法（如 `ILIKE`）；测试库是 SQLite。
9. **OpenAPI 代码优先**：路由必须走 `routekit.Group` + `Op` 元数据；禁止手写 swagger 注解或直接改 `docs/openapi.json`；改完运行 `make openapi` 并提交。API 契约变化后 `cd web/admin && pnpm gen:api`。漏元数据会被 `TestOpenAPICoversAllRoutes` 检出。
10. **鉴权默认拒绝**：`/api/v1` 除 `/auth/login`、`/auth/refresh` 外一律挂 `protected`（`Security(bearerAuth)` + `authkit.Authenticate`）。新增端点显式声明权限（`RequirePermission`）并在 `Op.Errors` 带 `CodeForbidden`；**401（未认证）与 403（无权限）不得混用**。资源归属校验放 service（`actor` 显式入参）。
11. **凭证与密钥**：密码只存 bcrypt；access token 无状态（`users.token_version` 控失效）；refresh token 只存 SHA-256、必须轮换 + 滑动续期，`session_started_at` 封顶会话；`refresh_reuse_interval` 只容忍 `revoked_reason=rotated` 的重放；登出/改密/禁用/超窗重用立即吊销全部会话。生产必须注入 `APP_AUTH_JWT_SECRET`（≥32 字节、非占位值）。细节见 `docs/design-auth.md`。
12. **后台任务与 goroutine**：**根 goroutine 只在 `cmd/server` 启动/回收**（`signal.NotifyContext` → `errgroup`/WaitGroup）。`internal/app` 只构造并暴露 worker，禁止在 app/service/handler/repository 起 fire-and-forget `go func()`。worker 必须 `Run(ctx) error`、可取消、clock/ticker 可注入、退出前等待在途任务、失败能取消 HTTP 并让进程非零退出；禁止裸 `go func()` 无回收、禁止 `time.Sleep` 代替 ctx select。细节见 `context.md` ADR 14 + `docs/design-message.md`。

规则细节入口：分层/依赖 `context.md` §2–§3；错误/校验 `apperr`/`httpx`；测试/质量门 `docs/agents/verification.md`；鉴权 `docs/design-auth.md`；goroutine `context.md` ADR 14。

## 3. 工作流

### 3.1 开工

1. `git status --short` 确认工作树；读 `.agents/local.md`（存在时）。
2. 读 `context.md` 相关章节 + 一个匹配的 `skills/<name>/SKILL.md`。
3. `make help` 查命令；缺工具 `make tools`（版本 pin 在 Makefile，禁止 `@latest`）。

### 3.2 标准改动

1. **定位**：context 相关章节 + skill。
2. **改代码**：遵守分层与命名；新增领域走 `add-domain`。
3. **补测试**：单测 + 集成测试；受保护端点覆盖 401/403/正常路径。
4. **验证**：`make check` 全绿；前端再跑 `pnpm api:check` + `pnpm build`。
5. **同步文档**：架构/契约/配置 → `context.md`；坑 → `docs/troubleshooting.md`；用户可见 → `README.md`；API → `make openapi` + `pnpm gen:api`。
6. **提交**：每次任务完成就提交，不攒工作树；按路径 `git add`，语义化 message。

更细的文档矩阵、提交拆分与推送规则：`docs/agents/workflow.md`。

### 3.3 质量门

| 门 | 何时跑 | 覆盖什么 |
|---|---|---|
| `make check` | **每次提交前（必跑）** | lint、gofmt、vet、race、覆盖率下限（`COVER_MIN`）、build、OpenAPI 黄金文件、tidy |
| `make audit` | auth/JWT/鉴权/迁移 SQL、发布前 | 密钥/日志卫生、可移植 SQL、迁移配对、401/403 回归、govulncheck |
| `make smoke` / `make verify-auth` | 有 postgres 时；auth 改动必跑 `verify-auth` | 真实进程 + 真实 PG 端到端 |

- **禁止未运行就声称通过**：贴真实输出最后几行。
- 前端改动：`make web-check`（eslint + vue-tsc + vitest + `pnpm api:check` + build 一次跑完）。
- 详细边界、证据要求、子集验证：`docs/agents/verification.md`。

### 3.4 子 agent（默认不拆）

拆不拆只看：**这个任务是否真的需要「独立上下文 + 独立视角」？** 不需要就在主会话做完。

- 只在 auth/鉴权改动、非平凡迁移、发布前安全审计等触发点拆。
- 子 agent **只读**：输出 `file:line` + 复现命令 + 严重度 + 建议；不改代码、不跑迁移、不提交。
- 规则写在文档里，不写在 agent 提示词里；提示词只写角色与输出格式。
- 成本要算清：独立进程，token/耗时翻倍且不共享缓存。

详见 `docs/agents/subagents.md`。

### 3.5 必须先问人（破坏性 / 不可逆）

- `git push --force`、改写已推送历史、删除远端分支。
- 删除/重写**已应用**的迁移文件；共享/预发/生产库 `make migrate-down`。
- `docker compose down -v`、删除数据卷、`rm -rf` 非构建产物。
- 提交密钥/`.env`、改仓库权限/分支保护。
- 超出任务范围的重构。

细节与安全替代：`docs/agents/destructive-operations.md`。

### 3.6 完成定义（DoD）

最终回复必须包含：

1. **变更文件清单**（按层：migration / entity / repo / service / handler / router / docs / frontend）。
2. **验证输出摘要**：`make check` 最后几行；auth/鉴权/迁移附 `make audit`；有 PG 附 `make smoke`/`verify-auth`；前端附 typecheck/build/api:check。
3. **未验证项与风险**：没环境/没跑的门明确写出来。
4. **提交记录**：`git log --oneline -1` + `git status -sb`（是否已推送）。

模板与反例：`docs/agents/commit-and-dod.md`。

## 4. 技能索引

| skill | 用途 |
|---|---|
| `add-domain` | 新增领域模块（迁移→repo→service→handler→routekit 路由→测试→OpenAPI） |
| `docker-ops` | 容器编排 + 本地端到端调试（也可用 `make smoke`） |
| `database-migrations` | 迁移新建/执行/回滚 + GORM model 同步 checklist |
| `sql-review` | 迁移/DDL 评审（锁与时长、可逆性、约束与软删、索引与查询匹配）+ 验证命令 |
| `security-review` | 机械安全门（`make audit`）+ 攻击面清单 + 黑盒攻防 curl 矩阵 |
