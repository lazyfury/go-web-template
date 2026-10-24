# 工作流细节

> 入口与强约束见 `../../AGENTS.md`。本文件只展开流程，不重复规则。

## 1. 开工

1. `git status --short`：先确认工作树里有没有别人的在制品。
2. 读 `.agents/local.md`（存在时）：本机 docker/proxy/工具路径差异。
3. 读 `context.md` 的任务相关章节：
   - 架构/依赖：§2、§3
   - 数据模型：§5
   - API 契约：§6
   - 扩展点：§10
   - 设计取舍：§9（ADR）
4. 任务匹配技能就加载 `skills/<name>/SKILL.md`；不要预读全部技能。
5. `make help` 查命令；工具缺失 `make tools`（版本 pin 在 Makefile，禁止 `@latest`）。

## 2. 标准改动

1. **定位**：读 context 相关章节 + 对应 skill。
2. **改代码**：遵守分层与命名（context §3/§10）；新增领域走 `add-domain`。
3. **补测试**：新领域必须有 service 层 mock repo 单测 + `internal/app` 全链路集成测试；受保护端点覆盖匿名 401、越权 403、正常路径。
4. **本地验证**：`make check` 全绿；前端改动再跑 `pnpm api:check` + `pnpm build`。
5. **同步文档**：见下方矩阵。
6. **提交**：每次任务完成就提交，不攒工作树；按文件路径 `git add`，语义化 message。
7. **回复**：贴真实输出与提交哈希，按 `commit-and-dod.md` 的 DoD 模板。

## 3. 文档同步矩阵

| 变化 | 更新到哪里 |
|---|---|
| 架构、依赖方向、数据模型、API 契约、配置、ADR | `context.md`（就地更新事实） |
| 新踩的坑 / 报错处理 | `docs/troubleshooting.md` |
| 用户可见行为、启动方式、特性 | `README.md` |
| 路由 / `routekit.Op` 元数据 | `make openapi` → 提交 `docs/openapi.json`；前端 `pnpm gen:api` |
| 可复用操作配方 | `skills/<name>/SKILL.md`（不要写进 agent 提示词） |
| agent 流程/约束变化 | `AGENTS.md` + 对应 `docs/agents/*.md` |
| 本机环境差异 | `.agents/local.md`（gitignored） |

## 4. 提交拆分

- 一个任务一个提交；不要把无关改动混进来。
- 功能与配置/文档同步能独立站住时分开提交。
- 用 `git worktree` 验证中间提交仍 `make check` 绿（需要时）。
- 只提交自己的路径；不要 `git add -A` 把别人的在制品带上。
- 不要改写已推送历史；`git push --force` 必须先问人。

示例：

```text
feat(message): add delivery worker, senders and server lifecycle
docs(message): document worker lifecycle and retry state machine
test(message): cover failed delivery reclaim
```

## 5. 推送

- 用户明确要求同步时才 `git push`。
- 推送前确认工作树干净、没有别人的在制品。
- 提交/推送后必须贴：
  - `git log --oneline -1`
  - `git status -sb`
- 不要声称“已提交/已推送”而不贴输出。
