---
name: add-domain
description: 在本 Go 单体模板中新增一个领域模块（如 posts/orders），按 model → 迁移 → repository → service → handler → router → 测试 → Swagger 的八步配方落地，保证分层、错误码、统一响应与测试策略与现有代码一致。
whenToUse: 用户要求新增业务领域/资源/CRUD 接口，或要求"像 users 那样加一个 X"时。
---

# 新增领域模块（以 posts 为例）

严格按以下顺序执行；每一步都参照现有 `users` 实现保持风格一致。

## 0. 准备

- 加载 `context.md` 第 3/5/6 节（包职责、数据模型、API 契约）。
- 命名：文件/结构体用领域名（`Post`），接口方法用动词（`Create/Get/List/Update/Delete`）。

## 1. model（`internal/model/post.go`）

```go
type Post struct {
    ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
    Title     string    `gorm:"type:varchar(128);not null" json:"title"`
    UserID    uint64    `gorm:"not null;index" json:"user_id"`
    CreatedAt time.Time `gorm:"not null" json:"created_at"`
    UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
    DeletedAt *time.Time `gorm:"index" json:"-"`
}
func (Post) TableName() string { return "posts" }
```

- 软删用 `*time.Time` + 查询走 GORM 默认 scope。
- 若模型有状态枚举，参考 `model.UserStatus`（int8 + 常量，常量要有导出注释）。

## 2. 迁移（`migrations/00000X_create_posts_table.{up,down}.sql`）

```bash
make migrate-create NAME=create_posts_table   # 生成两个空文件
```

up 要点：

```sql
CREATE TABLE IF NOT EXISTS posts (
    id         BIGSERIAL PRIMARY KEY,
    title      VARCHAR(128) NOT NULL,
    user_id    BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_posts_user_id ON posts (user_id);
CREATE INDEX IF NOT EXISTS idx_posts_deleted_at ON posts (deleted_at);
```

down：`DROP TABLE IF EXISTS posts;`。**GORM model 与 SQL 迁移必须同步**（双事实来源，见 context.md §9.8）。

## 3. repository（`internal/repository/post_repo.go`）

- 先定义接口 `PostRepository`（`Create/FindByID/List/Update/Delete`，首参 `context.Context`），再写 `postRepository` 实现 + `NewPostRepository(db)`。
- 错误一律包成 `apperrors.Wrap(apperrors.CodeDatabase, "...", err)`；记录不存在返回 `apperrors.ErrNotFound`；唯一冲突返回 `CodeConflict`。
- 查询保持可移植（`LOWER(col) LIKE ?`，不要 ILIKE）。
- 在 `internal/testutil/db.go` 的 `Models` 切片里追加 `&model.Post{}`（测试建表用）。

## 4. service（`internal/service/post_service.go`）

- 定义输入 DTO（`PostInput`/`PostPatch`——patch 用指针字段区分未传）与接口 `PostService`。
- 业务规则放这里：校验依赖关系（如 user 必须存在）、防止重复、软删逻辑。
- 禁止 import gin；依赖 `repository.PostRepository` 接口。

## 5. handler（`internal/handler/post.go`）

- 请求 DTO 用 binding tag：`binding:"required,min=2,max=128"`；校验失败 → `apperrors.Wrap(apperrors.CodeInvalidArgument, validationMessage(err), err)`。
- 每个方法补 Swagger 注解（参考 `user.go`：@Summary/@Tags/@Param/@Success/@Failure/@Router）。
- 响应一律 `response.OK/Created/NoContent/Fail`；ID 解析用 `parseID`（已在 handler 包内，直接复用）。

## 6. router 注册（`internal/router/router.go`）

```go
posts := v1.Group("/posts")
posts.POST("", h.Post.Create)
posts.GET("", h.Post.List)
posts.GET("/:id", h.Post.Get)
posts.PATCH("/:id", h.Post.Update)
posts.DELETE("/:id", h.Post.Delete)
```

- 同时把 `Post *handler.PostHandler` 加进 `router.Handlers`，并在 `internal/app/app.go` 完成装配（repo→service→handler→Handlers）。

## 7. 测试

- service 单测：`internal/service/post_service_test.go`，手写 mock repo（参考 `user_service_test.go` 的 `mockUserRepo`），覆盖业务规则分支。
- 集成测试：在 `internal/router/integration_test.go` 追加 CRUD 用例（复用 `newTestEngine`、`doJSON`、`decodeEnvelope`、`createUser` 帮助函数）。

## 8. 收尾验证

```bash
make vet && make lint && make test          # 必须全绿
make swagger                                 # 注解变更后重新生成 docs/
```

最后同步 `context.md`（§5 表结构、§6 API 契约）与 `README.md`（如用户可见），提交时包含 `docs/`。

## 注意

- 不要在 handler 包测试里 import router（import 循环）——集成测试放 router 包（`package router_test`）。
- 若新领域需要 join 查询，repository 里用 GORM Preload/Joins，保持 service 层不感知 SQL。
