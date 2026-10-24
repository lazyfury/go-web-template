# web/admin — 后台管理前端

go-web-template 的 Admin 前端模板，参考 [lazyfury/oliver `ui/admin`](https://github.com/lazyfury/oliver/tree/main/ui/admin) 的布局与功能形态重写，**修正了参考实现中的不规范范式，并重新打磨了 UI 视觉风格**。

## 技术栈

Vue 3 · TypeScript · Vite · Element Plus · Pinia · Vue Router · UnoCSS（原子类 + 按需图标）· ECharts（按需引入）· Axios

## 快速开始

```bash
cd web/admin
pnpm install
pnpm dev        # http://localhost:5173，/api 代理到 http://localhost:8080（Go 后端）
pnpm build      # 类型检查 + 生产构建到 dist/
pnpm gen:api    # docs/openapi.json → src/types/openapi.ts
```

登录走真实后端 `POST /api/v1/auth/login`（邮箱 + 密码）：access token 只存内存，refresh token 由后端写入 HttpOnly cookie；账号由管理员创建（`make seed-admin` 或后台「用户管理」）。

## 页面

| 路由 | 说明 | 数据 |
|---|---|---|
| `/login` | 登录页（左品牌区 + 右表单） | 真实 `POST /api/v1/auth/login` |
| `/dashboard` | 仪表盘：统计卡 + 销售趋势 + 渠道占比 + 最新用户 | 统计为演示值，最新用户走真实 API |
| `/users` | 用户管理：搜索 / 分页 / 新建 / 编辑 / 删除 | 全部对接真实 `/api/v1/users` |
| `/dicts` | 字典管理：左侧 group（setting）列表，右侧维护 dict items | 全部对接真实 `/api/v1/dict-groups`、`/api/v1/dict-items` |
| `/settings/basic` | 基础设置：站点名称/地址/Logo/联系方式等 | `GET` + `PUT /api/v1/dicts/site.basic` |
| `/settings/seo` | SEO 设置：标题/关键词/描述/robots/og:image | `GET` + `PUT /api/v1/dicts/site.seo` |
| 任意未匹配 | 404（后台布局内） | — |

## 目录结构

```
web/admin/src/
├── api/            # HTTP 层（http.ts：axios + 统一信封解包）与领域 API（user.ts / auth.ts / dict.ts）
├── components/
│   ├── common/     # 通用组件（StatCard / StatusTag）
│   └── layout/     # 布局组件（AppSidebar / AppHeader / AppTabs）
├── composables/    # usePagination（分页）/ useSettingGroup（整组 setting 读写）
├── config/         # menu.ts 侧栏菜单配置（与路由解耦）
├── layouts/        # AdminLayout 主布局
├── router/         # 静态路由表 + 登录守卫
├── stores/         # pinia：auth（登录态）/ tabs（多标签工作区）
├── styles/         # tokens.css 设计令牌 / base.css / element.css（EP 主题接入）
├── types/          # 类型与常量（api 信封 / user / auth / dict / openapi 生成）
└── views/          # 页面（Login / Dashboard / Users / Dicts / Settings / NotFound）
```

## 范式约定（相对参考项目的修正）

| 参考项目的问题 | 本模板的做法 |
|---|---|
| 菜单与路由职责混杂，动态注册路由 | 静态路由表（`router/routes.ts`）+ 菜单配置（`config/menu.ts`）解耦，菜单仅用于导航渲染 |
| pinia store 与 localStorage 双轨、按调用环境回退 | store 为登录态**单一事实来源**，localStorage 仅由 action 显式读写持久化 |
| `ApiPayload<T> \| T` 双形态 + 运行时 unwrap 探测 | 严格类型 `ApiEnvelope<T>`，http 拦截器统一解包，业务层直接拿到精确类型 |
| 全静态 class API 层、http 直接读 localStorage | 函数式 `request.get/post/patch/delete<T>`，token 通过 `setAuthTokenProvider` 注入（单向依赖） |
| 路由守卫 `matched.length===0 → return to.fullPath`（死循环隐患） | catch-all 404 路由，守卫逻辑收敛 |
| 登录页手写校验、`route.path` 精确判断布局 | el-form rules 校验；`meta.layout` 语义化 |
| 全量引入图标与 echarts | UnoCSS `i-ep-*` 按需图标类；echarts/core 按需注册 |
| `:root` 内嵌套 class（无效 CSS）、`!important` 清样式 | 设计令牌（`tokens.css`）统一收敛，Element Plus 通过 CSS 变量接入同源主题 |
| 每个视图重复 loading/try/catch 样板 | `usePagination` + `notifyError` 统一封装 |

## 样式体系

- **设计令牌**：`src/styles/tokens.css` —— 品牌色（Indigo）、中性色、圆角、阴影、布局尺寸全部为 CSS 变量，Element Plus 主题变量（`--el-color-primary` 等）映射同一套令牌，保证组件与自定义样式同源。
- **原子类**：UnoCSS 组合布局与间距；图标用 `i-ep-*` 类（按需编译）。
- **组件微调**：集中在 `element.css`，页面内不再散落 `:deep()` 覆盖。

## API 类型同步

后端是契约的唯一事实来源：

```bash
# 仓库根：改了路由/routekit.Op 之后
make openapi

# web/admin：从 docs/openapi.json 生成 TypeScript 类型
pnpm gen:api
```

- 生成物：`src/types/openapi.ts`（提交入库，业务层通过 `src/types/user.ts` 的别名使用）。
- 提交前在本地跑 `pnpm api:check`：重新生成并 `git diff --exit-code`，防止契约变更后忘记更新前端类型（仓库不配置远端 CI）。
- 业务代码不要手写重复的 `User` / `CreateUserPayload` 等接口，统一从 `components['schemas']` 取。

## 认证与权限

| 关注点 | 实现 |
|---|---|
| 登录 | `src/api/auth.ts` → `POST /auth/login`；响应 `{ access_token, token_type, expires_in, user{id,name,email,role,permissions} }` |
| 令牌存放 | access token **只在内存**（pinia store）；refresh token 在 HttpOnly + SameSite=Strict cookie（JS 不可读） |
| 刷新 | `src/api/http.ts`：401 → single-flight 调 `/auth/refresh` → 重放原请求一次；失败才跳登录 |
| 首屏恢复 | `stores/auth.ts#bootstrap()`（路由守卫调用）：用 cookie 换回 access token，刷新页面不掉线 |
| 权限 | `stores/auth.ts#can('users:write')`；菜单 `config/menu.ts` 的 `permission`、路由 `meta.permission`、按钮 `v-if` 三处共用同一套权限常量 |
| 类型 | `src/types/auth.ts` 全部来自 `openapi.ts` 生成的 schema，禁止手写重复接口 |

后端 RBAC：`admin` = `users:read/write/delete` + `dicts:read/write`；`member` = `users:read:self` + `users:write:self` + `dicts:read`。前端隐藏只是体验，**真正判定在服务端**（40100 未认证 / 40300 无权限）。

## 字典/配置页

`/settings/basic` 与 `/settings/seo` 是「整组 setting」表单：用 `useSettingGroup(group, defaults)` 读取 `GET /dicts/:group`，保存走 `PUT /dicts/:group`（object → items，整组替换，首次保存自动建组）。

`/dicts` 是 master-detail 布局：

- 左侧：group 列表（`key`/`name`/`status`/`item_count`），支持关键字搜索与分页；新建/编辑 group，`key` 创建后不可改。
- 右侧：选中 group 的 dict array；表格展示 `sort`/`key`/`label`/`value`/`value_type`/`status`/`remark`，支持新增/编辑/删除。
- 管理端请求带 `include_disabled=true`，停用的 group/item 在后台仍可见；消费侧 `GET /dicts/:group` 只返回启用项。
- 业务页面消费字典时用 `dictApi.values(group)`（或后端模块的 `dict.Store`），不要依赖管理端接口。

## 对接后端

开发环境 `vite.config.ts` 将 `/api` 代理到 `http://localhost:8080`。生产环境将 `dist/` 交给后端静态托管（或独立部署后把代理替换为同源部署）。
