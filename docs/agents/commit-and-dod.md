# 提交规范与完成定义（DoD）

> 入口见 `../../AGENTS.md`；验证命令见 `verification.md`。

## 提交

- 每次任务完成就提交，不攒工作树。
- 先 `git status --short` 复核。
- **按文件路径** `git add <paths>`；禁止 `git add -A`。
- 语义化 message：`feat|fix|chore|docs|test|refactor(scope): ...`。
- 一个任务一个提交；功能与配置/文档同步能独立站住时分开发。
- 只提交自己的路径；不要顺手带上别人的在制品。
- 用户要求同步时才 `git push`；推送前确认工作树干净。

提交示例：

```text
feat(message): add delivery worker, senders and server lifecycle
fix(sidebar): align sub-menu with top-level menu items
docs(agents): split AGENTS.md into workflow/verification volumes
test(message): cover failed delivery reclaim
```

## 最终回复（DoD）

必须包含：

1. **变更文件清单**（按层：migration / entity / repository / service / handler / router / docs / frontend）。
2. **验证输出摘要**：
   - `make check` 最后几行（`0 issues.`、测试、`tidy-check: ok`）；
   - auth/鉴权/迁移改动附 `make audit`；
   - 有 PG 时附 `make smoke` / `make verify-auth`；
   - 前端改动附 `pnpm typecheck` / `pnpm build` / `pnpm api:check`。
3. **未验证项与风险**：没有环境/没跑的门要明确写出来。
4. **提交记录**：`git log --oneline -1` + `git status -sb`；是否已推送。

## 不要做

- 不要写“已通过”“已提交”“已推送”而不贴真实输出。
- 不要把 `make test` 说成 `make check`。
- 不要用“应该”“大概”掩盖未验证项。
- 不要只列文件不说明未验证风险。
