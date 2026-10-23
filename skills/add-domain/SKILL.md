---
name: add-domain
description: 在本 Go 单体模板中新增一个领域模块（如 posts/orders），按 model → 迁移 → repository → service → handler → router → 测试 → Swagger 落地，保证分层、错误码、统一响应与测试策略和现有代码一致。用于"新增业务领域/资源/CRUD 接口"或"像 users 那样加一个 X"。
---

# 新增领域模块（以 posts 为例）

严格按顺序执行，每步对照现有 `users` 实现保持风格一致。开工前读 `context.md` §3（包职责）、§5（数据模型）、§6（API 契约）。

## 1. model（`internal/model/post.go`）

```go
type Post struct {
    ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
    Title     string     `gorm:"type:varchar(128);not null" json:"title"`
    UserID    uint64     `gorm:"not null;index" json:"user_id"`
    CreatedAt time.Time  `gorm:"not null" json:"created_at"`
    UpdatedAt time.Time  `gorm:"not null" json:"updated_at"`
    DeletedAt *time.Time `gorm:"index" json:"-"`
}

func (Post) TableName() string { return "posts" }
```

- 软删用 `*time.Time` + GORM 默认 scope。
- 状态枚举参考 `model.UserStatus`（int8 + 常量，每个导出常量都要注释，否则 `revive:exported` 会报）。

## 2. 迁移

**不要在本 skill 里复制迁移细节**：直接按 `skills/database-migrations/SKILL.md` 执行（新建 → up/down SQL → 同步 model → 加入 `testutil.Models`）。

要点：`user_id BIGINT NOT NULL REFERENCES users(id)`、时间列 `TIMESTAMPTZ NOT NULL DEFAULT now()`、外键与软删列建索引、up/down 互为逆操作。

## 3. repository（`internal/repository/post_repo.go`）

- 先定义接口（`Create/FindByID/List/Update/Delete`，首参 `context.Context`）+ `postRepository` 实现 + `NewPostRepository(db)`。
- 错误：DB 失败 → `apperrors.Wrap(apperrors.CodeDatabase, "...", err)`；记录不存在 → `apperrors.ErrNotFound`；唯一冲突 → `CodeConflict`。
- 查询保持可移植（`LOWER(col) LIKE ?`，不要 `ILIKE`）。
- 在 `internal/testutil/db.go` 的 `Models` 切片追加 `&model.Post{}`（测试建表用）。

## 4. service（`internal/service/post_service.go`）

- 输入 DTO（`PostInput`/`PostPatch`，patch 用指针字段区分"未传"）+ 接口 `PostService`。
- 业务规则放这里：依赖校验（如 user 必须存在）、防重复、软删语义。
- **禁止 import gin**；只依赖 `repository.PostRepository` 接口。

## 5. handler（`internal/handler/post.go`）

- 请求 DTO 用 binding tag：`binding:"required,min=2,max=128"`；失败 → `apperrors.Wrap(apperrors.CodeInvalidArgument, validationMessage(err), err)`。
- 每个方法补 Swagger 注解（照 `user.go`：@Summary/@Tags/@Param/@Success/@Failure/@Router）。
- 响应一律 `response.OK/Created/NoContent/Fail`；ID 解析复用包内 `parseID`。
- 构造函数参数名不要与 import 包同名（`svc service.PostService`，否则 `gocritic:importShadow`）。

## 6. router + 装配（`internal/router/router.go`、`internal/app/app.go`）

```go
posts := v1.Group("/posts")
posts.POST("", h.Post.Create)
posts.GET("", h.Post.List)
posts.GET("/:id", h.Post.Get)
posts.PATCH("/:id", h.Post.Update)
posts.DELETE("/:id", h.Post.Delete)
```

同时把 `Post *handler.PostHandler` 加进 `router.Handlers`，并在 `app.go` 完成 repo→service→handler 装配。

## 7. 测试

- service 单测：`internal/service/post_service_test.go`，手写 mock repo（参考 `user_service_test.go` 的 `mockUserRepo`），覆盖业务规则分支。
- 集成测试：在 `internal/router/integration_test.go` 追加 CRUD 用例，复用 `newTestEngine`、`doJSON`、`decodeEnvelope`、`createUser`。
- 每个测试通过 `testutil.NewSQLiteDB` 拿到独立内存库；不要依赖全局共享状态，也不要 `httptest.NewRequest`（用 `NewRequestWithContext`）。

## 8. 收尾

```bash
make check      # lint + gofmt + vet + race 测试 + build + docs 同步（必须全绿）
```

- 注解变更后 `make swagger` 并把 `docs/` 一起提交（`make check` 会检出未同步）。
- 同步 `context.md`（§5 表结构、§6 API 契约）与 `README.md`（用户可见变化）。
- 可在有 DB 的环境跑 `make smoke` 验证真实进程（无 DB 时只验 liveness）。

## 注意

- handler 包测试不要 import router（循环依赖）——集成测试放 `package router_test`。
- 新领域需要 join 查询时在 repository 用 GORM `Preload/Joins`，service 层不感知 SQL。
