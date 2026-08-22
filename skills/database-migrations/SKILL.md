---
name: database-migrations
description: go-web-template 的数据库迁移生命周期操作：golang-migrate 新建/执行/回滚/查看迁移、迁移与 GORM model 的同步 checklist、以及执行后的验证步骤。（表结构设计约定如软删/部分唯一索引见 context.md §5）
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

## 3. 与 GORM model 同步（双事实来源，checklist）

改表时四步：① 改 `migrations/00000X_*.sql`（生产 schema）→ ② 同步 `internal/model/<domain>.go` 的 GORM tag → ③ 新增表时把 `&model.X{}` 加进 `internal/testutil/db.go` 的 `Models`（测试建表）→ ④ 查询保持可移植（`LOWER(col) LIKE ?`，SQLite 测试不支持 ILIKE）。

表结构设计约定（软删、部分唯一索引等）见 context.md §5 与 ADR 4，不在此重复。

## 4. 验证

```bash
# 本地全链路验证（见 /docker-ops）：
docker compose -f deploy/docker-compose.yml up -d postgres
docker compose -f deploy/docker-compose.yml run --rm migrate   # 执行新迁移
# 进库抽查：
docker compose -f deploy/docker-compose.yml exec postgres psql -U postgres -d go_web_template -c '\d posts'
```

`make test` 不依赖迁移（测试用 AutoMigrate），但**集成/仓储测试必须覆盖新表的 CRUD 与软删行为**。
