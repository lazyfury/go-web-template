# AGENTS.md

本仓库面向 AI 代理的协作约定：

- **操作规范**（硬性规则、常用命令、本机环境坑、变更流程）→ 读 [agent.md](agent.md)
- **项目上下文**（架构、数据流、配置、API 契约、设计决策、扩展指南）→ 读 [context.md](context.md)
- **可复用操作配方** → 加载 `skills/`（或 `.agents/skills/`，符号链接）下的 skill：

| skill | 用途 |
|---|---|
| `add-domain` | 新增领域模块（八步配方） |
| `run-checks` | 提交前质量检查（fmt/vet/lint/test） |
| `debug-e2e` | 本地端到端调试与排查 |
| `docker-deploy` | 镜像/compose/CI 构建部署 |
| `database-migrations` | 迁移生命周期管理 |

改动代码前请先读 `agent.md` 与 `context.md`；涉及架构/API/配置变化时同步更新 `context.md`。
