---
name: database-migrations
description: go-web-template 的数据库迁移生命周期管理：golang-migrate 新建/执行/回滚迁移、验证 schema、软删除与唯一索引的注意点、GORM model 与 SQL 迁移的同步要求。
whenToUse: 用户要求改表结构、加新表、回滚迁移、检查迁移状态，或新建迁移文件时。
---

# 数据库迁移管理

工具：golang-migrate（SQL 文件，`migrations/00000X_name.{up,down}.sql`）。生产 schema 的唯一事实来源是迁移 SQL，**不是** GORM AutoMigrate（后者只服务测试库，见 context.md §9.8）。

## 1. 新建迁移

```bash
make migrate-create NAME=add_posts_table     # 生成 00000X_add_posts_table.{up,down}.sql
```

- 命名用 snake_case 动词开头：`create_xxx_table` / `add_column_to_xxx` / `drop_xxx`。
- 一个迁移只做一件事，可回滚：up/down 必须互为逆操作。
- up 幂等写法：`CREATE TABLE IF NOT EXISTS`、`CREATE INDEX IF NOT EXISTS`、`ADD COLUMN IF NOT EXISTS`。

## 2. 执行 / 回滚 / 查看

```bash
make migrate-up            # 应用全部未执行的迁移
make migrate-down          # 回滚最近 1 个迁移
# 查看状态：
docker compose -f deploy/docker-compose.yml run --rm migrate \
  -path=/migrations -database 'postgres://postgres:postgres@postgres:5432/go_web_template?sslmode=disable' version
```

- 重复执行 up 输出 `no change` = 正常。
- 回滚是危险的：确认没有下游依赖再 down；CI/生产流程一般只 up。

## 3. 软删除与唯一索引（本模板约定）

- 表带 `deleted_at TIMESTAMPTZ`；删除 = 置 deleted_at，行保留审计。
- **唯一约束必须用部分唯一索引**：`CREATE UNIQUE INDEX ... ON users (email) WHERE deleted_at IS NULL;`——否则软删后无法复用邮箱。
- 普通查询（GORM 默认 scope）自动过滤已删行；`Unscoped()` 可查含已删行（慎用）。

## 4. 与 GORM model 同步（双事实来源）

改表时 checklist：

1. 改 `migrations/00000X_*.sql`（生产 schema）。
2. 同步 `internal/model/<domain>.go` 的 GORM tag（列名/类型/索引/软删字段）。
3. 新增表时把 `&model.X{}` 加进 `internal/testutil/db.go` 的 `Models`（测试建表）。
4. 查询保持可移植：`LOWER(col) LIKE ?`（SQLite 测试不支持 ILIKE）。

## 5. 验证

```bash
# 本地全链路验证（见 /debug-e2e）：
docker compose -f deploy/docker-compose.yml up -d postgres
docker compose -f deploy/docker-compose.yml run --rm migrate   # 执行新迁移
# 进库抽查：
docker compose -f deploy/docker-compose.yml exec postgres psql -U postgres -d go_web_template -c '\d posts'
```

`make test` 不依赖迁移（测试用 AutoMigrate），但**集成/仓储测试必须覆盖新表的 CRUD 与软删行为**。
