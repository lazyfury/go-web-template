# 破坏性 / 不可逆操作

> 入口见 `../../AGENTS.md`。下列操作**先问人**，不要自行执行。

## 必须先确认

- `git push --force`、改写已推送历史、删除远端分支。
- 删除或重写**已应用**的迁移文件（只允许新增向前迁移）。
- `make migrate-down` 作用在共享 / 预发 / 生产库。
- `docker compose down -v`、删除数据卷、`rm -rf` 非构建产物。
- 提交密钥 / `.env` / 证书；修改仓库权限或分支保护。
- 超出任务范围的重构、批量重命名、依赖大版本升级。

## 提问前准备

1. 明确目标与影响范围（表/服务/环境）。
2. 给出精确命令（不要在提问里用模糊的“清理一下”）。
3. 给出回滚/备份方案（快照、导出、worktree、旧镜像）。
4. 说明可接受的停机时间与验证方式。

## 安全替代

| 想做的 | 更安全的做法 |
|---|---|
| 改已应用迁移 | 新增 forward migration，保留旧迁移不动 |
| 在共享库试 down | 本地/临时库或 `worktree` + 独立 compose 项目 |
| 清理容器数据 | `docker compose down`（不带 `-v`），先备份卷 |
| 大规模重构 | 拆成多个可独立验证的小提交，先问范围 |
| 提交本地配置 | 用 `.env` / `.agents/local.md`（gitignored），提交 `.env.example` |

## 迁移边界

- 生产 schema 只以 `migrations/*.sql` 为准；GORM AutoMigrate 只服务测试。
- 迁移必须 up/down 成对且 down 非空；回滚只保证 schema，不保证数据。
- 数据回填/大表 DDL 走 `sql-review` + 发布窗口。
