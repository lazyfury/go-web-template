---
name: add-domain
description: 在本 Go 单体模板中新增一个领域模块（如 posts/orders）：迁移 → internal/<domain> 一个包内 entity/repository/service/handler/routes → app 装配 → 单测 + 集成测试 → Swagger。用于"新增业务领域/资源/CRUD 接口"或"像 users 那样加一个 X"。
---

# 新增领域模块（以 posts 为例）

一个域 = **一个包** `internal/post`，包内按文件分层（entity → repository → service → handler → routes）。
不要新建 `internal/model`、`internal/repository` 之类横向包。开工前读 `context.md` §2（依赖方向）、§3（包职责）、§5（数据模型）、§6（API 契约）。

## 1. 迁移（先做 schema）

按 `.agents/skills/database-migrations/SKILL.md` 执行：`make migrate-create NAME=create_posts_table` → 写 up/down SQL → 跑 `make migrate-up`（无 DB 时至少保证 SQL 语法与逆操作正确）。

要点：外键 `user_id BIGINT NOT NULL REFERENCES users(id)`、时间列 `TIMESTAMPTZ NOT NULL DEFAULT now()`、软删/外键建索引。

## 2. entity（`internal/post/entity.go`）

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
- 状态枚举参考 `user.Status`（int8 + 常量；每个导出常量都要注释，否则 `revive:exported` 报错）。
- **不要**拆成"纯 domain struct + 独立 persistence model"两套：模板阶段是一个 GORM 实体，注释写清列含义即可。

## 3. repository（`internal/post/repository.go`）

```go
// Repository 是持久化契约（消费方 service 依赖它，测试用 fake 替换）。
type Repository interface {
    Create(ctx context.Context, p *Post) error
    FindByID(ctx context.Context, id uint64) (*Post, error)
    List(ctx context.Context, offset, limit int) ([]Post, int64, error)
    Update(ctx context.Context, p *Post) error
    Delete(ctx context.Context, id uint64) error
}

type gormRepository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &gormRepository{db: db} }
```

- 首参 `context.Context`，查询一律 `r.db.WithContext(ctx)`。
- 错误：DB 失败 → `apperr.Wrap(apperr.CodeDatabase, "...", err)`；记录不存在 → `apperr.ErrNotFound`；唯一冲突 → `CodeConflict`。
- 查询保持可移植（`LOWER(col) LIKE ?`，不要 `ILIKE`）。

## 4. service（`internal/post/service.go`）

```go
type Service struct{ repo Repository }

// 返回具体类型（Go 惯例：return structs, accept interfaces）。
func NewService(repo Repository) *Service { return &Service{repo: repo} }
```

- 输入 DTO：`Input` / `Patch`（patch 用指针字段区分"未传"）。
- 业务规则放这里：依赖校验（如 user 必须存在 → 注入 `user.Repository` 或定义窄接口）、防重复、软删语义。
- **禁止 import gin**；也不要 import `internal/web`。
- 不要再定义 `PostService` 接口：repo 接口已是测试缝，handler 依赖 `*Service` 即可。

## 5. handler（`internal/post/handler.go`）

- `type Handler struct{ svc *Service }` + `func NewHandler(svc *Service) *Handler`。
- 请求 DTO 用 binding tag：`binding:"required,min=2,max=128"`；失败 → `apperr.Wrap(apperr.CodeInvalidArgument, validationMessage(err), err)`。
- 每个方法补 Swagger 注解（照 `user/handler.go`：@Summary/@Tags/@Param/@Success/@Failure/@Router）；`@Success {object} httpx.Body`。
- 响应一律 `httpx.OK/Created/NoContent/Fail`；ID 解析复用包内 `parseID`。
- 构造函数参数名不要与 import 包名同名（用 `svc`，否则 `gocritic:importShadow`）。

## 6. routes（`internal/post/routes.go`）

```go
func RegisterRoutes(rg *gin.RouterGroup, h *Handler) {
    posts := rg.Group("/posts")
    posts.POST("", h.Create)
    posts.GET("", h.List)
    posts.GET("/:id", h.Get)
    posts.PATCH("/:id", h.Update)
    posts.DELETE("/:id", h.Delete)
}
```

路由挂在**域内**：`internal/web` 不 import 业务域，避免循环依赖。

## 7. 装配（`internal/app/app.go` → `NewWithDB`）

```go
postRepo := post.NewRepository(db)
postService := post.NewService(postRepo)
postHandler := post.NewHandler(postService)
post.RegisterRoutes(v1, postHandler)
```

`v1` 是 `web.New` 返回的 `/api/v1` 组（已带 RateLimit + Timeout）。

## 8. 测试

- service 单测：`internal/post/service_test.go`，手写 fake `Repository`（参考 `user/service_test.go` 的 `mockRepository`），覆盖规则分支。
- repository 测试：`internal/post/repository_test.go`，`testutil.NewSQLiteDB(t, &post.Post{})`（**显式传 model**）。
- 集成测试：在 `internal/app/integration_test.go` 追加 CRUD 用例，复用 `newTestHandler`、`doJSON`、`decodeEnvelope`、`createUser`。
- 每个测试用独立命名内存库；`httptest.NewRequestWithContext`（不要 `httptest.NewRequest`，`noctx` 会报）。

## 9. 收尾

```bash
make check      # lint + gofmt + vet + race + build + docs 同步（必须全绿）
```

- 改了注解 → `make swagger` 并提交 `docs/`（`make check` 的 `swagger-diff` 会检出漂移）。
- 同步 `context.md`（§5 表结构、§6 API 契约）与 `README.md`（用户可见变化）。
- 有 DB 时跑 `make smoke`；无 DB 时只验证 liveness 路径并如实说明。

## 注意

- `internal/web` 只能被动接收 `RegisterRoutes`，不能反向 import 域包。
- 需要 join 时在 repository 用 GORM `Preload/Joins`，service 不感知 SQL。
- 新表记得同时更新迁移 SQL 与 entity（双事实来源），测试库不跑迁移、只跑 AutoMigrate。
