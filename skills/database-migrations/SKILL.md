---
name: database-migrations
description: go-web-template 的数据库迁移生命周期：golang-migrate 新建/执行/回滚/查看迁移，迁移与 GORM model 的同步 checklist，以及执行后的验证。用于"改表结构、加表、回滚迁移、检查迁移状态、新建迁移文件"。
---

# 数据库迁移管理

工具：golang-migrate（SQL 文件，`migrations/00000X_name.{up,down}.sql`）。**生产 schema 的唯一事实来源是迁移 SQL**，不是 GORM AutoMigrate（后者只服务测试库，见 `context.md` ADR 8）。

`make migrate-*` 会自动选择执行方式：有 docker 用 `migrate/migrate` 镜像，否则用本地 `migrate` 二进制（`make migrate-tool` 安装，带 postgres 驱动）。

## 1. 新建迁移

```bash
make migrate-create NAME=add_posts_table     # 生成 00000X_add_posts_table.{up,down}.sql
```

- 命名 snake_case 动词开头：`create_xxx_table` / `add_column_to_xxx` / `drop_xxx`。
- 一个迁移只做一件事，up/down 必须互为逆操作。
- up 幂等写法：`CREATE TABLE IF NOT EXISTS`、`CREATE INDEX IF NOT EXISTS`、`ADD COLUMN IF NOT EXISTS`。

## 2. 执行 / 回滚 / 查看

```bash
make migrate-up          # 应用全部未执行迁移（重复执行输出 "no change" = 正常）
make migrate-down        # 回滚最近 1 个（破坏性，共享库上必须先问人）
make db-up               # 起本地 postgres（docker compose 优先，否则检查本机 postgres）
```

查看版本（有 docker 时）：

```bash
docker compose -f deploy/docker-compose.yml run --rm migrate \
  -path=/migrations -database 'postgres://postgres:postgres@postgres:5432/go_web_template?sslmode=disable' version
```

无 docker 时：`migrate -path migrations -database 'postgres://postgres:postgres@127.0.0.1:5432/go_web_template?sslmode=disable' version`。

## 3. 与 GORM model 同步（双事实来源 checklist）

1. 改 `migrations/00000X_*.sql`（生产 schema）。
2. 同步 `internal/<domain>/entity.go` 的 GORM tag（类型、索引、软删）。
3. 测试建表：调用处显式传 model，例如 `testutil.NewSQLiteDB(t, &user.User{}, &post.Post{})`（`testutil` 不再维护全局 `Models` 列表，避免 import cycle）。
4. 查询保持可移植：`LOWER(col) LIKE ?`（SQLite 测试不支持 `ILIKE`）。

表结构设计约定（软删、部分唯一索引等）见 `context.md` §5 与 ADR 4。

## 4. 验证

```bash
make migrate-up
# 有 docker：
docker compose -f deploy/docker-compose.yml exec postgres psql -U postgres -d go_web_template -c '\d posts'
# 无 docker（本机 psql 且连 127.0.0.1）：
psql 'postgres://postgres:postgres@127.0.0.1:5432/go_web_template?sslmode=disable' -c '\d posts'
make migrate-roundtrip  # 真实 PG scratch 库：up → down -all → up（先 make db-up；证明 down 可回滚）
make smoke      # 有 DB 时覆盖真实进程的 CRUD 全链路
```

`make test` 不依赖迁移（测试用 AutoMigrate），但仓储/集成测试必须覆盖新表的 CRUD 与软删行为。

## 注意

- 回滚是危险操作：确认没有下游依赖再 down；部署流程只 up。
- 已应用到共享环境的迁移文件不要改写——新增迁移修正。
- 报错速查（compose 卷相对路径、容器连不上库、SQLite 语法差异）见 `docs/troubleshooting.md`。
