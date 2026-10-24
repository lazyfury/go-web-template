# AGENTS.md — AI 协作约定与操作手册

> 适用于任何在本仓库工作的 agent（也适用于人类协作者）。
> **从仓库根启动会话**：项目技能通过 `.agents/skills/` 暴露，会话目录不在仓库根时不会被发现。

## 事实来源分工（避免同一规则写三遍）

| 文件 | 只放什么 |
|---|---|
| `AGENTS.md`（本文件） | **规则、流程、完成定义**；不抄命令、不抄操作细节 |
| `context.md` | **架构、数据模型、API 契约、配置、ADR**（就地更新事实，不写排错） |
| `skills/` | **可执行配方**（命令 + 断言），按任务加载 |
| `docs/troubleshooting.md` | **踩坑 → 处理**（报错速查、环境陷阱） |
| `Makefile` | **命令的唯一权威**（看 `make help`；README/AGENTS 不复写） |
| `.agents/local.md` | 本机环境差异（gitignored；存在则先读） |

## 项目一句话

Go Web 单体应用模板：**Gin + GORM + PostgreSQL + zap**，分层 `handler → service → repository`，统一响应信封 + 业务错误码，JWT 登录 + RBAC，内置 Docker / OpenAPI（routekit 生成）/ Prometheus。

## 开工顺序

1. 读 `context.md` 的相关章节（§2 架构、§3 包职责、§5 数据模型、§6 API 契约）。
2. 任务匹配技能就加载对应 `skills/<name>/SKILL.md`（索引见文末）。
3. `make help` 查命令；工具缺失先 `make tools`。

## 硬性规则（违反 = 打回）

1. **分层与依赖方向**：业务包内 `handler → service → repository`（同一包内按文件分层）；包级方向 `cmd → app → {web, auth, user, platform/*}`，`platform/*` 是叶子包。`auth → user` 只允许单向（经包内 `UserStore` 接口）。禁止反向 import；service 不得 import gin；`web` 不得 import 业务域（域用 `RegisterRoutes` 自己挂载）；`internal/` 不得被外部非 main 包 import。
2. **错误处理**：业务错误一律 `internal/platform/apperr`（`apperr.Wrap/New` + 稳定 Code）；handler 用 `httpx.Fail(c, err)` 输出 `{code,message,data}`，不返回裸 error、不自造 JSON。
3. **校验**：请求 DTO 用 gin binding tag；`ShouldBindJSON` 失败 → `apperr.CodeInvalidArgument`。
4. **日志**：只用 zap（`logx.New` 构建）；访问日志由中间件产出，业务代码不 print。
5. **上下文**：service/repository 方法首参必须是 `context.Context`，透传 `c.Request.Context()`；DB 查询 `WithContext(ctx)`。
6. **测试**：新领域必须带 service 层 mock repo 单测 + `internal/app` 全链路集成测试（`app.NewWithDB`）；测试不得依赖真实 PostgreSQL。受保护端点要覆盖匿名 401、越权 403、正常路径；用 `app.SeedAdmin` + `loginAsAdmin` 取 token。
7. **依赖**：新增依赖后 `go mod tidy`；sqlite 只允许出现在测试路径（`internal/testutil`）。
8. **可移植 SQL**：不用 PostgreSQL 专属语法（如 `ILIKE`），测试库是 SQLite。
9. **OpenAPI 代码优先**：路由必须通过 `routekit.Group` 注册并带 `Op` 元数据；禁止手写 swagger 注解或直接改 `docs/openapi.json`（改完运行 `make openapi` 并提交）。API 契约变化后同步前端类型（`cd web/admin && pnpm gen:api`）。新增端点漏元数据会被 `TestOpenAPICoversAllRoutes` 检出。
10. **鉴权默认拒绝**：`/api/v1` 下除 `/auth/login`、`/auth/refresh` 外一律挂在 `protected` 组（`Security(bearerAuth)` + `authkit.Authenticate`）。新增端点必须显式声明权限（`authkit.RequirePermission`）并在 `Op.Errors` 里带 `CodeForbidden`；**401（未认证）与 403（无权限）不得混用**。资源归属校验放 service（`actor` 显式入参），不要只靠中间件。禁止把密码/token 写日志或放进 DTO。
11. **凭证与密钥**：密码只存 bcrypt 摘要；access token 无状态（`users.token_version` 控失效）；refresh token 只存 SHA-256、必须轮换 + **滑动续期**，并用 `session_started_at` 封顶整个会话；`refresh_reuse_interval`（默认 30s）只容忍 `revoked_reason=rotated` 的重放（多标签页竞态），登出/改密/禁用/超窗重用一律立即吊销全部会话。生产必须注入 `APP_AUTH_JWT_SECRET`（≥32 字节、非占位值）。

## 质量门（唯一入口）

```bash
make check      # = golangci-lint + gofmt 校验 + vet + race 测试 + build + OpenAPI 黄金文件/依赖 同步
```

- 质量门全部在本地执行（仓库不配置远端 CI）：提交前必须自己跑 `make check`；前端改动再跑 `pnpm api:check` 与 `pnpm build`。
- 工具缺失 → `make tools`（版本 pin 在 `Makefile` 顶部，**不要 `@latest`**）。
- 改了路由/`routekit.Op` 元数据 → `make openapi` 并提交 `docs/openapi.json`（`make check` 的黄金测试会检出漂移）。
- 只改 Go 代码时，最快闭环是 `make test`：测试用内存 SQLite，**不需要 postgres/docker**。
- **禁止未运行就声称通过**：结论要有真实输出支撑。
- **附加门 `make audit`**（不并入 `make check`：含 govulncheck，慢且要网络）：改动涉及 **auth/JWT/鉴权/迁移 SQL** 或**发布前**必须跑。它只覆盖机械可判定部分（密钥泄露、日志卫生、可移植 SQL、迁移 up/down 配对、鉴权 401/403 矩阵回归、依赖漏洞）。

## 子 agent 使用边界（默认不拆）

拆不拆只看一条：**这个任务是否真的需要「独立上下文 + 独立视角」？** 不需要就一律在主会话做完。

| 场景 | 做法 |
|---|---|
| 计划 | 主会话（读 `context.md` + 对应 skill）。计划要与实现上下文连续，隔离后容易做偏 |
| 开发 | 主会话。并发写同一仓库 = 冲突 + 主会话不知道改了什么 |
| 测试验证 | 主会话跑 `make test` / `make check`。**结论只认脚本输出**，不接受任何 agent 自述“已跑过” |
| SQL 审核 | 机械部分 `make audit`；判断部分走 `sql-review` skill；只有非平凡迁移（大表 DDL、数据回填、不可逆 down）才另开**只读**子 agent 复核 |
| 安全审计 | 值得拆：全仓扫描上下文大，且必须独立于实现者视角。跑 `security-review` skill 的清单 |
| 黑盒攻防测试 | 用例矩阵先固化成断言（`scripts/smoke.sh` / `make audit`）保证可回归；子 agent 只负责「还能怎么打」的发散与结果解读 |

用子 agent 时的硬约束（违反 = 打回）：

1. **只读**：只输出 `file:line` + 复现命令 + 严重度 + 建议；不改代码、不跑迁移、不提交。改代码回主会话。
2. **规则写在文档里，不写在 agent 提示词里**：`agents/*.md` 只写角色与输出格式，要遵守的规则引用本文件与对应 skill，避免第四个真相源。
3. **触发点有限**：auth/鉴权改动、迁移改动、发布前；不是每次提交。
4. **算清成本**：pi 的子 agent 是独立 `pi` 进程（`examples/extensions/subagent`，需自行安装），token 与耗时翻倍且不共享缓存。单次 `make check` 能覆盖的事不要付这份钱。

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
- 提交密钥/`.env`、修改仓库权限或分支保护。
- 改动超出任务范围的重构（先确认再动）。

## 完成定义（DoD）

最终回复必须包含：

1. **变更文件清单**（按层：model / repository / service / handler / router / docs）。
2. **`make check` 的真实输出摘要**（贴最后几行，不要只写"已通过"）；改动涉及 auth/鉴权/迁移 → 附 `make audit` 输出。
3. **未验证项与风险**（例如"本机无 docker，未跑 compose e2e；已用 `./scripts/smoke.sh` 验证 liveness 路径"）。

## 技能索引

| skill | 用途 |
|---|---|
| `add-domain` | 新增领域模块（迁移→repo→service→handler→routekit 路由→测试→OpenAPI） |
| `docker-ops` | 容器编排 + 本地端到端调试（也可用 `make smoke` 一条命令覆盖） |
| `database-migrations` | 迁移新建/执行/回滚 + GORM model 同步 checklist |
| `sql-review` | 迁移/DDL 评审（锁与时长、可逆性、约束与软删、索引与查询匹配）+ 验证命令 |
| `security-review` | 机械安全门（`make audit`）+ 攻击面清单 + 黑盒攻防 curl 矩阵 |
