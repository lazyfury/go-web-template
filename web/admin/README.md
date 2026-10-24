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

登录页为**演示模式**（mock）：任意用户名 + 4 位以上密码即可登录，见下方「接入真实认证」。

## 页面

| 路由 | 说明 | 数据 |
|---|---|---|
| `/login` | 登录页（左品牌区 + 右表单） | mock，可切换真实接口 |
| `/dashboard` | 仪表盘：统计卡 + 销售趋势 + 渠道占比 + 最新用户 | 统计为演示值，最新用户走真实 API |
| `/users` | 用户管理：搜索 / 分页 / 新建 / 编辑 / 删除 | 全部对接真实 `/api/v1/users` |
| 任意未匹配 | 404（后台布局内） | — |

## 目录结构

```
web/admin/src/
├── api/            # HTTP 层（http.ts：axios + 统一信封解包）与领域 API（user.ts / auth.ts）
├── components/
│   ├── common/     # 通用组件（StatCard / StatusTag）
│   └── layout/     # 布局组件（AppSidebar / AppHeader / AppTabs）
├── composables/    # usePagination（分页状态封装）
├── config/         # menu.ts 侧栏菜单配置（与路由解耦）
├── layouts/        # AdminLayout 主布局
├── router/         # 静态路由表 + 登录守卫
├── stores/         # pinia：auth（登录态）/ tabs（多标签工作区）
├── styles/         # tokens.css 设计令牌 / base.css / element.css（EP 主题接入）
├── types/          # 类型与常量（api 信封 / user / auth / openapi 生成）
└── views/          # 页面（Login / Dashboard / Users / NotFound）
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
- CI 会跑 `pnpm api:check`：重新生成并 `git diff --exit-code`，防止契约变更后忘记更新前端类型。
- 业务代码不要手写重复的 `User` / `CreateUserPayload` 等接口，统一从 `components['schemas']` 取。

## 接入真实认证

1. `src/api/auth.ts`：将 `USE_MOCK` 置为 `false`，按后端契约调整 `realLogin` 的路径与响应字段（当前 `POST /auth/login`）。
2. 登录响应需返回 `{ token, user: { id, username, email, role } }`（见 `src/types/auth.ts`）。
3. 如需菜单/权限接口，在 `src/config/menu.ts` 与路由守卫中扩展（参考 `filterMenusByPermissions` 的思路，但保持静态路由不变）。

## 对接后端

开发环境 `vite.config.ts` 将 `/api` 代理到 `http://localhost:8080`。生产环境将 `dist/` 交给后端静态托管（或独立部署后把代理替换为同源部署）。
