# Agent 分册索引

> `AGENTS.md` 是入口（强约束 + 工作流 + 导航），本目录放**细节**。
> 渐进式披露：默认只读 `AGENTS.md` + 任务相关的 1–2 个分册，不要一次读完。

## 分册

| 文件 | 内容 | 何时读 |
|---|---|---|
| [`workflow.md`](workflow.md) | 开工顺序、标准改动、文档同步、提交拆分、分支/推送 | 开始一个任务、准备提交时 |
| [`verification.md`](verification.md) | `make check` / `make audit` / `make smoke` / `verify-auth` 的边界、命令、证据要求 | 每次提交前、发布前 |
| [`subagents.md`](subagents.md) | 什么时候拆子 agent、只读约束、输出格式、成本 | 考虑拆分独立审查时 |
| [`destructive-operations.md`](destructive-operations.md) | 必须先问人的操作、回滚/备份要求、安全替代 | 动迁移/数据/历史/密钥前 |
| [`commit-and-dod.md`](commit-and-dod.md) | commit 规范、原子提交、DoD 模板、最终回复格式 | 准备提交、写最终回复时 |
| [`../security-review-2026-10.md`](../security-review-2026-10.md) | 上一次黑盒演练的发现/复现/修复记录与残留风险 | 改 auth/限流/部署配置时对照 |

## 默认阅读顺序

1. 每次会话：`AGENTS.md` → `.agents/local.md`（存在时）→ `git status --short`。
2. 开工：`context.md` 的任务相关章节 → `skills/<name>/SKILL.md`。
3. 提交前：`verification.md` → `commit-and-dod.md`。
4. 其他分册只在对应场景加载。

## 事实来源提醒

- 架构/数据/API/配置/ADR：`../../context.md`（不要在本目录重复）。
- 可执行配方：`../../skills/<name>/SKILL.md`（不要写进 agent 提示词）。
- 命令：`../../Makefile`（`make help` 是唯一权威）。
- 报错速查：`../troubleshooting.md`。
