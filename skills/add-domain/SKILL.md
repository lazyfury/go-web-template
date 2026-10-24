---
name: add-domain
description: 在本 Go 单体模板中新增一个领域模块（如 posts/orders）：迁移 → internal/<domain> 一个包内 entity/repository/service/handler/routes → app 装配 → 单测 + 集成测试 → routekit OpenAPI 元数据。用于"新增业务领域/资源/CRUD 接口"或"像 users 那样加一个 X"。
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
- **不要写 Swagger 注解**：请求/响应类型与错误码统一在 `routes.go` 的 `routekit.Op` 里声明，OpenAPI 由启动时反射生成。
- 响应一律 `httpx.OK/Created/NoContent/Fail`；ID 解析复用包内 `parseID`。列表等复合响应用具体结构体（如 `ListResponse`），不要用 `gin.H`，否则 spec 的 `data` 会退化成空对象。
- 响应 DTO 放 `internal/<domain>/dto.go`（如 `UserDTO`），不要直接返回 GORM entity；持久化字段（软删时间等）不应出现在 API/OpenAPI。
- 构造函数参数名不要与 import 包名同名（用 `svc`，否则 `gocritic:importShadow`）。

## 6. routes（`internal/post/routes.go`）

```go
func RegisterRoutes(rg *routekit.Group, h *Handler) {
    rg.Registry().DescribeTag("posts", "Post management")
    posts := rg.Group("/posts", routekit.Tags("posts"))

    posts.POST("", &routekit.Op{
        ID:        "createPost",
        Summary:   "Create a post",
        Request:   routekit.JSON[CreateRequest](true),
        Responses: []routekit.Response{routekit.Created[Post]("created post")},
        Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeConflict},
    }, h.Create)

    posts.GET("", &routekit.Op{
        ID:      "listPosts",
        Summary: "List posts",
        Params: []routekit.Param{
            routekit.Query[int]("page", "Page number", false),
            routekit.Query[int]("page_size", "Page size", false),
        },
        Responses: []routekit.Response{routekit.OK[ListResponse]("post page")},
    }, h.List)

    posts.GET("/:id", &routekit.Op{
        ID:        "getPost",
        Summary:   "Get a post",
        Params:    []routekit.Param{routekit.Path[uint64]("id", "Post ID")},
        Responses: []routekit.Response{routekit.OK[Post]("post")},
        Errors:    []apperr.Code{apperr.CodeInvalidArgument, apperr.CodeNotFound},
    }, h.Get)
    // PATCH / DELETE 同理；DELETE 用 routekit.NoContent()
}
```

要点：

- 路径参数必须在 `Op.Params` 里用 `routekit.Path[T]` 声明，否则 `Build()` 会失败。
- `Errors` 里的 `apperr.Code` 会自动映射 HTTP 状态并生成错误信封；413/429/500 默认注入。
- 路由挂在**域内**：`internal/web` 不 import 业务域，避免循环依赖。
- **本模板的域默认要求登录**：`internal/app` 把域挂在 `protected` 组上（已带 `Security(bearerAuth)` + `authkit.Authenticate`）。域内再用权限子组声明授权：
  ```go
  adminOnly := rg.Group("/posts", routekit.Middleware(authkit.RequirePermission(authkit.PermUsersWrite)))
  ```
  `Op.Errors` 补 `apperr.CodeForbidden`（401 由 routekit 按 `Security` 自动补）。归属校验（"只能改自己的"）放 service，`actor authkit.Principal` 显式入参。

## 7. 装配（`internal/app/app.go` → `NewWithDB`）

```go
postRepo := post.NewRepository(db)
postService := post.NewService(postRepo)
postHandler := post.NewHandler(postService)
post.RegisterRoutes(api, postHandler)
```

`api` 是 `web.New` 返回的 `*routekit.Group`（已带 `/api/v1` 的 RateLimit + Timeout 与 OpenAPI 注册表）。所有域注册完后，`app.NewWithDB` 会调用 `web.MountDocs` 生成 `/openapi.json` 与 Swagger UI；域内不要自己挂文档端点。

## 8. 测试

- service 单测：`internal/post/service_test.go`，手写 fake `Repository`（参考 `user/service_test.go` 的 `mockRepository`），覆盖规则分支。
- repository 测试：`internal/post/repository_test.go`，`testutil.NewSQLiteDB(t, &post.Post{})`（**显式传 model**）。
- 集成测试：在 `internal/app/integration_test.go` 追加 CRUD 用例，复用 `newTestEnv`、`doJSONAuth`、`decodeEnvelope`、`createUser`、`env.loginAdmin(t)`；**必须**覆盖匿名 401 与越权 403。
- 权限常量：`internal/platform/authkit/rbac.go` 加 `Permission` + 角色映射，并在 `internal/app/openapi_test.go` 的安全断言里保持受保护操作带 `bearerAuth`/401/403。
- 每个测试用独立命名内存库；`httptest.NewRequestWithContext`（不要 `httptest.NewRequest`，`noctx` 会报）。

## 9. 收尾

```bash
make check      # lint + gofmt + vet + race + build + OpenAPI 黄金测试 + 依赖 tidy（必须全绿）
```

- 改了路由或 `Op` → `make openapi` 并提交 `docs/openapi.json`（`make check` 的黄金测试会检出漂移）。
- API 契约变化后同步前端类型：`cd web/admin && pnpm gen:api`，提交 `src/types/openapi.ts`。
- 同步 `context.md`（§5 表结构、§6 API 契约）与 `README.md`（用户可见变化）。
- 有 DB 时跑 `make smoke`；无 DB 时只验证 liveness 路径并如实说明。

## 注意

- `internal/web` 只能被动接收 `RegisterRoutes`，不能反向 import 域包。
- 不要把新域挂在公开的 `api` 组上（除登录/刷新这类必须公开的端点）；公开端点要在 OpenAPI 安全测试里显式列出。
- 需要 join 时在 repository 用 GORM `Preload/Joins`，service 不感知 SQL。
- 新表记得同时更新迁移 SQL 与 entity（双事实来源），测试库不跑迁移、只跑 AutoMigrate。
