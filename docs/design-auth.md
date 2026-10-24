# 认证与权限设计（提案 · 未实现）

> 状态：**已实现（P0–P4）**，P5 加固项（登录指标/清理任务/密钥轮换）待做。
> 代码入口：`internal/auth`（域）、`internal/platform/authkit`（JWT/RBAC/中间件）、`internal/platform/passwd`（bcrypt）、
> `internal/app/app.go`（装配）、`migrations/000002~000003`、`web/admin/src/{api,stores}/auth.ts`。
> 架构/契约的权威事实仍以 `context.md`（§2/§5/§6/§9/§11）为准；本文档只保留设计取舍与威胁模型。
> 触发背景：当前 `/api/v1/users` **完全无鉴权**（`grep -rn "Authorization\|jwt" internal cmd` 仅命中 CORS 白名单），
> 前端 `web/admin` 是 `USE_MOCK = true` 的演示登录。本设计给出可落地的最小真实闭环。

## 1. 目标与范围

| 项 | 决定 |
|---|---|
| 认证方式 | 邮箱 + 密码登录，签发 **短期 JWT access token** + **可吊销 refresh token** |
| 传输 | access token 走 `Authorization: Bearer <jwt>`；refresh token 走 **HttpOnly Cookie**（见 §13 待决 1） |
| 授权 | RBAC：`admin` / `member` 两级角色 + 代码内权限表；不做 DB 驱动的动态权限（可演进） |
| 默认策略 | **`/api/v1` 下除 `/auth/login`、`/auth/refresh` 外全部要求登录**（"默认拒绝"） |
| 不做 | 第三方 OAuth、短信/邮箱验证码、多租户、分布式会话、细粒度资源 ACL |

### 端点鉴权矩阵

| 端点 | 登录 | 权限 |
|---|---|---|
| `POST /api/v1/auth/login` | 否 | 公开，按 IP+邮箱限流 + 失败锁定 |
| `POST /api/v1/auth/refresh` | 否（凭 refresh cookie） | 轮换 + 重用检测 |
| `POST /api/v1/auth/logout` | 是 | 吊销当前会话 |
| `GET /api/v1/auth/me` | 是 | 本人 |
| `PATCH /api/v1/auth/password` | 是 | 本人，成功后吊销其他会话 |
| `GET /api/v1/users` | 是 | `users:read`（admin 全量；member 仅自己 → 见 §7） |
| `GET /api/v1/users/:id` | 是 | `users:read` + 本人或 admin |
| `POST /api/v1/users` | 是 | `users:write`（admin） |
| `PATCH /api/v1/users/:id` | 是 | `users:write` 或本人（仅 name/email） |
| `DELETE /api/v1/users/:id` | 是 | `users:delete`（admin） |
| `/healthz`、`/readyz` | 否 | 探针必须免认证 |
| `/metrics` | 否（保持现状） | 部署层限制来源；如需鉴权另开 admin listener |
| `/openapi.json`、`/swagger/*` | 否 | 由 `swagger.enabled` 控制，生产关闭 |

## 2. 令牌模型

### Access token（JWT，HS256）

```jsonc
{
  "iss": "go-web-template",
  "sub": "42",              // user id（字符串，符合 RFC 7519）
  "role": "admin",
  "ver": 3,                 // users.token_version：改密/禁用即失效
  "jti": "0f1c…",
  "typ": "access",
  "iat": 1730000000,
  "exp": 1730000900         // 15m
}
```

- 校验必须显式钉死算法：`jwt.WithValidMethods([]string{"HS256"})` + `WithIssuer` + `WithLeeway(30s)`；禁止从 header 取 `alg`（防 alg=none / RS256 混淆）。
- 密钥：`APP_AUTH_JWT_SECRET`，要求 ≥32 字节随机；`app.env=production` 时缺失/过短 **启动失败**，开发环境缺省值打 warn。
- access token 不落库、不可吊销；吊销靠 `ver`（改密、禁用、强制下线 +1）→ 最坏延迟 = access TTL。

### Refresh token（不透明随机串）

- 32 字节 `crypto/rand` → base64url；**只存 SHA-256 摘要**（`token_hash CHAR(64) UNIQUE`），DB 泄露不可直接使用。
- TTL 30 天 **滑动**（每次轮换续满）；`session_started_at` 跨轮换继承，会话绝对上限 90 天。
- 每次 `/auth/refresh` **轮换**：旧 token 置 `revoked_at` + `revoked_reason=rotated`，新 token 返回。
- 复用宽限：`revoked_reason=rotated` 且吊销在 30s 内 → 容忍（多标签页共享 cookie jar），发新 token 并清理兄弟 token，保持仅 1 个活跃 token。
- 重用检测：超窗重放、或登出/改密等非轮换吊销 → **吊销该用户全部 refresh token** 并返回 401（登出立即生效，不被宽限期复活）。

## 3. 数据模型（迁移 + entity 双同步）

`000002_add_auth_fields_to_users.up.sql`

```sql
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(32) NOT NULL DEFAULT 'member';
ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS failed_login_attempts SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_users_role ON users (role);
```

`000003_create_refresh_tokens.up.sql`

```sql
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  CHAR(64) NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    user_agent  VARCHAR(255),
    ip          VARCHAR(64),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_active
    ON refresh_tokens (user_id) WHERE revoked_at IS NULL;
```

`000004_add_refresh_session_start.up.sql`

```sql
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS session_started_at TIMESTAMPTZ NOT NULL DEFAULT now();
-- rotated / logout / reuse / superseded / password / admin / session_cap
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS revoked_reason VARCHAR(32);
```

- 现有用户 `password_hash = ''` → **无法登录**（等价于无凭证）；上线需 §12 P1 的 bootstrap 管理员。
- 定期清理：`DELETE FROM refresh_tokens WHERE expires_at < now() - interval '7 days'`（放 `cmd/` 的 cron 或后续 job）。
- 迁移 SQL 与 GORM entity 同步（`context.md` §5 双事实来源）；测试库 `AutoMigrate` 需显式加新 model。

## 4. 包结构与依赖方向

```
internal/auth/                  # 新业务域（同包分文件）
  dto.go         LoginRequest{Email,Password} / TokenResponse / MeResponse / ChangePasswordRequest
  service.go     Login / Refresh / Logout / VerifyAccess / ChangePassword / SeedAdmin
  repository.go  RefreshTokenRepository（Create/FindByHash/Revoke/Rotate/RevokeAllForUser）
  handler.go     login/refresh/logout/me/change-password
  routes.go      RegisterRoutes(rg *routekit.Group, h *Handler)  ← 公开端点挂 api，受保护端点挂 protected
internal/platform/authkit/      # 叶子包：纯能力，不 import 任何业务域
  token.go       Issue/Parse（HS256 钉死、iss/leeway）
  principal.go   Principal{UserID, Role, TokenVersion}
  middleware.go  Authenticate(Verifier) / RequireRole / RequirePermission
  rbac.go        role → permission 映射表
```

依赖方向：`app → {auth, user, web, platform/*}`；`auth → platform/{authkit,apperr,httpx,routekit}`；`auth` 通过**包内窄接口**消费用户数据（`user.Repository` 结构化满足），不反向依赖 auth；`authkit` 不 import `auth`/`user`，中间件所需的"查库校验"通过接口注入：

```go
// authkit
type Verifier interface {
    VerifyAccess(ctx context.Context, raw string) (*Principal, error)
}
```

`auth.Service` 实现 `VerifyAccess`（解析 JWT + 校验 `users.status`、`token_version`）。`platform` 保持叶子性质，`web` 仍不 import 业务域。

## 5. API 契约（routekit 元数据）

- `web.New` 注册安全方案（`routekit.Config.SecuritySchemes`，routekit 已支持）：
  `bearerAuth = {type: http, scheme: bearer, bearerFormat: JWT}`。
- 受保护路由用 `routekit.Security("bearerAuth")`（Group 选项，已存在）；Op 自动带 401，权限组再显式带 403。
- 401 与 403 语义严格区分：无/坏/过期 token → `40100`；身份有效但权限不足 → `40300`。
- 需要新增 `apperr` 码时优先复用现有 40100/40300/42900，**不新增码**。
- 契约变更后 `make openapi` + 前端 `pnpm gen:api`（`LoginRequest/TokenResponse/MeResponse` 自动生成 TS 类型，替代手写 `types/auth.ts`）。

请求/响应示例：

```
POST /api/v1/auth/login      {"email":"admin@example.com","password":"…"}
200 {"code":0,"message":"ok","data":{"access_token":"…","token_type":"Bearer","expires_in":900,"user":{"id":1,"name":"Admin","email":"…","role":"admin","permissions":["users:read","users:write","users:delete"]}}}
401 {"code":40100,"message":"invalid credentials","data":null,"request_id":"…"}   # 不区分"用户不存在/密码错"
423? → 复用 40300 + message "account locked, retry after …"（避免为锁定新增码）
```

## 6. 密码与登录安全

| 项 | 决定 |
|---|---|
| 哈希 | bcrypt cost 12（`golang.org/x/crypto/bcrypt`）；后续可切 argon2id（`auth.algo` 配置预留） |
| 密码长度 | 8–72 字节（bcrypt 截断上限，超过 72 直接 400，避免静默截断） |
| 用户枚举 | 未知邮箱也执行一次 bcrypt（对固定 dummy hash），响应与耗时不区分；错误消息统一 `invalid credentials` |
| 锁定 | 连续 5 次失败 → `locked_until = now()+15m`；成功登录清零；锁定期间直接 40300（不跑 bcrypt） |
| 登录限流 | 独立令牌桶（IP + email 维度，如 5/min），复用 `web` 的 limiter 实现并在 `/auth/login` 路由组上挂 |
| 审计 | `last_login_at`、`failed_login_attempts`；登录成功/失败写结构化日志（**不含密码/token**） |
| 禁用账号 | `users.status=0` → 登录拒绝，且 `VerifyAccess` 直接 401（token 立即失效） |
| 改密 | 校验旧密码 → 更新 hash + `password_changed_at` + `token_version++` → 吊销其他 refresh token |
| 传输 | 生产必须 HTTPS（反代终止）；Cookie `Secure`、`HttpOnly`、`SameSite=Strict`、`Path=/api/v1/auth` |

禁止事项：日志打印 `Authorization`/密码/token（现有 `Logger` 只记 method/path/status/UA/IP，保持）；token 放 URL query；把 JWT 写进 localStorage（见 §10）。

## 7. RBAC（权限范围）

代码内静态表，起步小、易读：

```go
// authkit/rbac.go
var rolePermissions = map[Role][]Permission{
    RoleAdmin:  {"users:read", "users:write", "users:delete", "auth:manage"},
    RoleMember: {"users:read:self", "users:write:self"},
}
```

- `admin`：用户 CRUD 全量 + 管理会话（后续可加 `users:impersonate`）。
- `member`：只能读/改自己（`/auth/me`、`PATCH /users/:id` 仅本人且仅 `name`/`email`）。
- 越权判定在 **service**（需要 principal，不只是中间件）：handler 从 `authkit.PrincipalFrom(c)` 取；`service.Update` 校验 `principal.ID == id || role == admin`。
- 中间件只做粗粒度门（`RequirePermission("users:write")`），资源归属放 service —— 单一事实来源，避免中间件与业务规则两处判断。
- 演进：需要动态权限时把 `rolePermissions` 换成 `roles` / `role_permissions` 表 + 启动缓存；接口不变。

## 8. 配置项

```yaml
auth:
  jwt_secret: ""            # APP_AUTH_JWT_SECRET；生产必填，>=32 字节
  issuer: go-web-template
  access_ttl: 15m
  refresh_ttl: 720h            # 滑动：每次 refresh 续满
  refresh_max_lifetime: 2160h  # 会话绝对上限（从首次登录算）
  refresh_reuse_interval: 30s  # 轮换后旧 token 的容忍窗口（多标签页竞态）
  bcrypt_cost: 12
  login_max_attempts: 5
  login_lockout: 15m
  cookie_secure: true       # 本地 http 调试时置 false
```

`config.Validate()` 增补：生产环境 `jwt_secret` 空/`<32` 字节 → 报错；`access_ttl > 0`、`refresh_ttl > access_ttl`、`bcrypt_cost ∈ [10,14]`。同步 `configs/config.example.yaml`、`README.md` 环境变量表、`.env.example`。

## 9. 需要的 routekit 小扩展（约 20 行 + 单测）

`routekit.Group` 目前只聚合元数据，不能挂中间件（`rg` 未导出）。新增：

```go
func Middleware(mw ...gin.HandlerFunc) GroupOption // 写入 groupConfig
// Group(): 建好子 *gin.RouterGroup 后 child.rg.Use(cfg.middleware...)
```

这样组合根可以：

```go
auth.RegisterRoutes(api, authHandler)                                  // 公开
protected := api.Group("", routekit.Security("bearerAuth"),
    routekit.Middleware(authkit.Authenticate(authService)))
user.RegisterRoutes(protected, userHandler)                            // 全部要求登录
adminOnly := protected.Group("", routekit.Middleware(authkit.RequirePermission("users:write")))
```

替代方案（不推荐）：在 `web.New` 给整个 `/api/v1` 挂认证中间件 + 公开路径白名单 —— 白名单与路由表两处维护，容易漏。

配套：`routekit` 在 `Op.Security` 非空时自动补 401 响应（可在 `registry.Add` 里做），并用单测锁死"受保护操作必含 401"。

## 10. 前端改造（`web/admin`）

| 文件 | 改动 |
|---|---|
| `src/api/http.ts` | 401 → **单飞**（single-flight）调 `/auth/refresh` 后重放原请求一次；失败才 `handleAuthExpired`。`withCredentials: true`（refresh cookie） |
| `src/stores/auth.ts` | access token **只存内存**（不写 localStorage）；localStorage 仅存可公开的 `user`（用于刷新后首屏）；启动时先用 refresh 换 access |
| `src/api/auth.ts` | 删 `USE_MOCK` 与 mock；`login({email,password})`、`refresh()`、`logout()`、`me()`、`changePassword()` |
| `src/types/auth.ts` | 改为从 `src/types/openapi.ts` 取 `components['schemas']`（登录/用户/权限） |
| `src/router/index.ts` | 守卫增加 `to.meta.permission` 判定（无权限 → 403 页或 dashboard） |
| `src/config/menu.ts` + `UsersView.vue` | 菜单/按钮按 `permissions` 过滤（`v-if="auth.can('users:write')"`） |
| `vite.config.ts` | 开发期 `/api` 代理天然同源 cookie，无需放宽 CORS |

CORS 注意：refresh 走 cookie 时必须 `allow_credentials=true` 且 `allowed_origins` 不能是 `*`（浏览器规则）。当前默认 `*` + `false`，若前后端跨域部署需同时改（推荐用同源反代规避）。

## 11. 测试矩阵

- **authkit 单测**：签发/解析、过期、错误 iss、`alg=none`/RS256 混淆、错误密钥、超长 token、401 响应形状。
- **auth service 单测**（fake repo/clock）：登录成功、密码错、未知邮箱（同样慢且同样错误）、禁用、锁定、锁定过期、refresh 轮换、**宽限期内重放容忍（只清理兄弟 token）**、**宽限期外重用撤销全部**、logout、改密后 `token_version++` 且撤销其他会话。
- **中间件单测**：无 Authorization / 非 Bearer / 坏 token / 过期 / 禁用用户 → 401；member 调 admin 接口 → 403；principal 正确注入 ctx。
- **集成测试**（`internal/app`，生产同款装配）：login → 带 token CRUD 200；无 token 401；member 越权 403；DELETE 后 token 仍有效但资源 404；改密后旧 access 401。
- **OpenAPI**：`TestOpenAPICoversAllRoutes` 基础上新增断言——除白名单外所有 `/api/v1` 操作必须 `Security` 含 `bearerAuth` 且文档化 401。
- **前端**：`pnpm api:check`（类型未漂移）、`pnpm build`、401 刷新重放的手动路径。
- **smoke**：`scripts/smoke.sh` 增加 login 取 token、带 token 跑 CRUD、并断言匿名 401（参照现有 `fail()` 风格）。
- **verify-auth（真实进程 + 真实 PG）**：`make verify-auth` 覆盖宽限期、登出立即生效、`X-Refresh-Transport`/`X-Refresh-Token` 传输、会话绝对上限夹紧与 `session_cap` 防御分支——这些行为无法用 SQLite 集成测试背书。

## 12. 分阶段实施（每阶段独立可上线）

| 阶段 | 内容 | DoD |
|---|---|---|
| P0 | 配置项 + 迁移 000002/000003 + entity/DTO 字段（不改变行为） | `make check` 绿；`make migrate-up` 幂等 |
| P1 | bcrypt 工具 + `cmd/admin` 建管理员 + auth service（login 可用，未强制） | service 单测覆盖失败/锁定；可 curl 拿到 token |
| P2 | JWT 签发 + `authkit` 中间件 + **users 全量要求登录** + OpenAPI 安全方案 + smoke 更新 | 匿名 401、带 token 200；golden 更新；集成测试补登录 |
| P3 | refresh 轮换/重用检测 + logout + `/auth/me` + 改密 + cookie | 并发 refresh 单飞；改密后旧 token 401 |
| P4 | RBAC 中间件 + 归属校验（member 仅自己）+ 前端权限化 UI + 移除 mock | member 越权 403；前端按权限隐藏按钮 |
| P5 | 登录限流/锁定指标、`/metrics` 计数、清理任务、密钥轮换预案 | `make vuln` 无新增高危；文档同步 |

P2 是破坏性节点：现有集成测试与 `scripts/smoke.sh` 都会因 401 失败，必须同一提交内更新（否则 `make check` 红）。

## 13. 待决事项（已按推荐项落地）

1. **refresh token 载体**：HttpOnly Cookie（推荐，抗 XSS，需处理 CSRF/SameSite）vs 响应体 + 前端存储（简单，XSS 可偷）。
2. **RBAC 粒度**：两级角色（推荐先做）vs DB 驱动角色/权限表（多一轮迁移与缓存设计）。
3. **开户策略**：仅管理员/CLI 创建（推荐，管理后台场景）vs 公开注册 + 邮箱验证（需要邮件通道）。
4. **TTL**：access 15m / refresh 滑动 30 天 + 绝对上限 90 天（推荐）vs access 24h / 无 refresh（更简单，但无法吊销）。
5. **`/metrics` 是否要求认证**：保持公开（内网）vs 单独 admin 端口（推荐生产）。

## 14. 风险与已知限制

- access token 无状态：吊销/禁用最坏延迟 = access TTL（15m）；改密可即时失效（`token_version`）。
- 每次请求多一次 DB 查询校验状态（`users.status`/`token_version`）；高 QPS 需加本地缓存（带短 TTL），模板阶段先不做。
- 登录限流仍是单实例内存态（与现有 `RateLimit` 同问题），多副本需 Redis。
- HS256 单密钥：轮换需多密钥 + `kid`（后续扩展），否则轮换瞬间全量掉线。
- 用户表加了敏感列，`UserDTO` 已隔离，但 `SELECT *` 的仓储查询要把 `password_hash` 保持 `json:"-"` 且不做 DTO 透传。
- 现有用户 `password_hash=''`，升级后需 bootstrap；忘记密码流程本期不做（管理员重置）。
