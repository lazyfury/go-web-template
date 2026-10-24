---
name: sql-review
description: go-web-template 的 SQL / 迁移评审配方：机械门（make audit 的迁移配对、down 非空、可移植 SQL 检查）+ 判断清单（锁与时长、可逆性、NOT NULL 与回填、唯一索引与软删、索引与查询匹配、GORM model 同步）+ 本地验证命令。用于"审核迁移 SQL、评审 DDL、检查索引/约束、上线前数据变更评审"。
---

# SQL 与迁移评审

迁移的创建 / 执行 / 回滚流程见 `database-migrations` skill；本文件只管**评审**。

## 1. 机械门

```bash
make audit       # 覆盖：up/down 成对、down 非空（空 down 回滚必炸）、internal/ 无 PG 专属语法
make test        # SQLite 上跑真实查询，可移植性的最终判据
```

`make audit` 绿了只代表"不会明显炸"，不代表设计对。下面才是评审本体。

## 2. 判断清单

**可逆与顺序**

- `down` 是否真的能回到上一版？drop 列/表会丢数据——**丢数据的 down 可以接受，但必须在文件头注释写清"不可逆"**，且不能用于已上线的共享库。
- 一个迁移只做一件事；`up` 与 `down` 是否互为逆操作？`CREATE ... IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS` 是否幂等？
- 编号是否连续、名字是否动词开头（`add_column_to_xxx`）？已应用的迁移**不得改写**——新增一个修正它。

**锁与时长（生产 5 分钟超时就够毁一次发布）**

- 大表 `ALTER TABLE` 是否会拿长锁（加列带默认值、改类型、加 `NOT NULL` 无默认）？需要时拆成「加列 → 分批 backfill → 加约束」三个迁移。
- 加索引是否阻塞写入？golang-migrate 默认单事务执行，`CREATE INDEX CONCURRENTLY` 在事务里**会失败**——要么接受短锁，要么单独迁移 + 显式非事务执行，并在 PR 说明。
- backfill 是否在小批次里做（避免长事务 + WAL 膨胀）？是否有可跑的中间状态（旧代码仍能工作）？

**约束与数据模型**

- `NOT NULL` 是否配了默认值或先 backfill？否则已有行会失败。
- 唯一约束与软删（`deleted_at`）是否冲突？需要部分唯一索引（only where deleted_at is null），GORM 侧用 `uniqueIndex` + `where` 同步。
- 外键是否显式 `ON DELETE`？软删模型下物理外键常与软删冲突——如果不用外键，写清由 service 保证引用完整性。
- 类型与精度：金额是否 `numeric(18,2)`（不是 float）、时间是否 `timestamptz`、枚举是否用 check 约束或 smallint？
- 命名：索引 `idx_<table>_<cols>`、唯一 `uq_...`、外键 `fk_...`；列 snake_case。

**同步 GORM model（双事实来源）**

- `internal/<domain>/entity.go` 的 tag 是否与迁移一致（类型、`index`、`uniqueIndex`、软删字段）？
- 新表是否在用到的测试里显式传 model 给 `testutil.NewSQLiteDB(t, &post.Post{})`（不要新增全局 Models 列表）？
- 查询是否还能在 SQLite 跑（`LOWER(col) LIKE ?`，禁 `ILIKE`）？— `make test` 会直接暴露。

## 3. 验证（有 docker 时做全链路，没有就标注"未验证"）

```bash
make db-up
make migrate-up                          # 期望：应用到新版本，重复跑输出 "no change"
make migrate-down && make migrate-up     # 验证 down 真的能回滚（本地库，绝不在共享库跑）
make test                                # SQLite：可移植性 + 仓储 CRUD/软删
make smoke                               # 真实进程 + 真实 PG：CRUD 全链路
```

模型/索引是否落库：

```bash
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U postgres -d go_web_template -c '\d <table>'
```

## 4. 输出格式

```
[必须改/建议/可选] 文件名:行号
问题: 一句话（例：00000X 给 users.email 加唯一索引会与软删冲突）
依据: 已有行 / SQLite 差异 / 锁行为 / GORM tag 不同步
改法: 具体 SQL 片段或拆分成哪几个迁移
```

**边界**：评审阶段只读；`make migrate-down` 只在本地库跑；改写或删除**已应用**的迁移必须先问人（`AGENTS.md`「必须先问人」）。
