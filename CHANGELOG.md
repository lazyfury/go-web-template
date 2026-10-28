# Changelog

本仓库是模板（template），版本号用 `vMAJOR.MINOR.PATCH` 打 tag；`Unreleased` 段记录尚未发版的改动。
格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，事实以 `git log` 为准。

## [Unreleased]

### Added

- `make migrate-roundtrip`：在 scratch 库上跑 `up → down -all → up`，证明 down SQL 真的可回滚
  （`audit` 只检查文件成对/非空）。已在真实 PG 上验证，并做过「故意写坏 down」的负向验证。
- `make e2e` + Playwright 冒烟：真实浏览器 + 后端 + PG 跑「登录 → 新建用户 → 删除用户 → 退出登录」，
  退出后再访问受保护页会被守卫弹回登录页。
- 可观测性：`build_info{version,commit}`、`db_pool_*`（database/sql 池统计）、
  `worker_last_success_timestamp_seconds{worker}`（投递/清理 worker 的存活时间戳）；
  配套 `MessageWorkerStalled` / `HousekeepingStalled` 告警与两个看板面板。
- 配置：`database.slow_threshold`（慢查询阈值，默认 1s）、`message.mail.require_tls`
  （默认 true，production 不允许 false）。
- 前端覆盖率门槛（v8，仅统计 store/composable/api/router/utils：stmts 40 / branches 35 / funcs 25 / lines 40）。

### Changed

- gorm 日志改用自实现 `gormlogger.Interface`：SQL 失败落 **error**、慢查询落 warn（阈值可配）、
  成功语句落 info，字段结构化；`ParamsFilter` 保留，SQL 仍以占位符记录。
- Go 覆盖率门拆成三层：总量 `COVER_MIN`（76 → 78）、每包下限 `COVER_PKG_MIN`（55）、
  关键文件（auth/限流/sender/用户仓储）不允许 0% 函数。`isUniqueViolation` 等路径补测。
- 前端 Element Plus 按需自动引入（`unplugin-vue-components` / `unplugin-auto-import`），
  `manualChunks` 改为函数式只归类稳定大依赖：JS 1674KB → 1136KB（gzip 565 → 386KB）。
- `dbx.New` 用 5s 超时的 `PingContext` 做启动检查（黑洞地址不再挂住启动）。

### Fixed

- 邮件发送强制 STARTTLS：`message.mail.require_tls` 默认 true，服务器未宣告 STARTTLS 时拒绝发送；
  无 TLS 连接下拒绝发送 SMTP 凭据。`sendSMTP` 从 0% 覆盖到有假 SMTP 服务器的完整测试。
- 终态投递清理缺索引：新增 `000009` 部分索引 `(updated_at) WHERE status IN (2,4,5)`，
  真实 PG（20 万行 / 47MB、每小时清理约 1800 行）实测 31.1ms → 0.5ms（Seq Scan → Index Scan）。
- UnoCSS 每次构建刷 `failed to load icon "-"`：`blocklist` 屏蔽 `-` / `i--` 这两个非图标 token。

## [0.2.0] - 2026-10-28

### Added

- `make ci`：把 `check` + `web-check` + `audit` 收敛成一个全量入口，本地提交前与接入 CI 都只调它；
  顺带 `make web-install`（frozen lockfile）与 `make web-check` 的前置依赖检查（缺依赖时给可执行提示）。
- 配置三方一致性测试（`internal/platform/config/parity_test.go`）：`Config` 结构体 tag ↔
  `setDefaults` ↔ `configs/config.example.yaml` 双向比对，并对示例配置跑一遍 `Validate`。
- `LICENSE`（MIT）：README 早就声明 MIT，但仓库里没有授权文件。
- 路由矩阵测试：每个已注册路由都声明「匿名 401 / member / admin / 非法 JSON 400」，
  新路由未分类即失败（`internal/app/route_matrix_test.go`）。
- `make cover-check`（覆盖率下限 `COVER_MIN`，默认 74）并入 `make check`。
- `/readyz` 校验 `schema_migrations` 版本与 dirty 位，漏跑/半跑迁移时 503；
  `dbx.RequiredSchemaVersion` 与迁移文件的最大编号由单测钉死；启动日志打印 applied/required。
- `http.max_header_bytes`（默认 16 KiB）：`http.Server` 不再退回 stdlib 的 1 MiB 头预算。
- 投递 worker 每次发送前条件续租（`ErrLeaseLost` 即跳过），避免慢 channel 下多副本重复投递；
  配置层强制 `message.lease >= message.delivery_timeout`。
- 前端门禁 `make web-check`：ESLint 10 + vitest(jsdom) + vue-tsc + `pnpm api:check` + build；
  新增 tabs store 与「菜单 ↔ 路由权限一致性」测试。
- `deploy/prometheus/alerts.yml` + `deploy/grafana/dashboard.json` + `deploy/observability/README.md`。
- `make image-scan`（trivy，HIGH/CRITICAL）。

### Fixed

- CORS 配置校验：`cors.allow_credentials=true` 时拒绝 `*`（含空列表回落到 `*`）、拒绝
  `https://*.example.com` 通配模式、要求每项是 `scheme://host[:port]` 形式的 origin；逗号分隔的
  环境变量会被 trim。此前这些组合都能启动成功，然后在浏览器里静默失效（refresh cookie 流程中断），
  或让 `cors.New` 在装配期 panic。
- `configs/config.example.yaml` 补上缺失的 13 个键（`ratelimit.backend`、`redis.*`、`retention.*`）；
  `.env.example` 一直把该文件当“完整键列表”，但保留期清理与多副本限流在那里是查不到的。
- 退出登录不再「看起来没反应」：原来 `authApi.logout()` 是 fire-and-forget，导航到 `/login` 时
  store 里还留着 access token，守卫又把它弹回 `/dashboard`，直到下一次请求 401 才被动重定向。
  现在 `useLogout` 先等后端吊销 + 本地清态，再带 `redirect` 跳登录页（并发点击只执行一次，
  后端不可达也照样离开当前页）。
- 401 语义收窄：`/auth/login`、`/auth/refresh`、`/auth/logout`、`/auth/password` 上的 401 是业务结果
  （改密时「当前密码错误」也是 401），不再触发刷新重放、清会话和跳登录——否则输错一次当前密码
  就会被当成掉线踢回登录页。
- 登录页 `?redirect=` 与「登录过期」跳转统一走 `safeRedirectTarget`：只接受站内路径，
  拒绝 `//evil.example`、外部 URL、控制字符与回到 `/login` 自身；并发 401 只跳一次，不再把
  `redirect` 叠成 `/login?redirect=/login?...`。
- 侧栏只按父级权限过滤：无 `messages:manage` 的角色会看到模板/投递入口，点进去被守卫弹回仪表盘。
  现在子项各自声明权限并按权限过滤，父级只在有可见子项时渲染。
- 投递领取的第二条腿（租约过期的 sending 行）缺少索引，真实 PG 上退化为全表扫描；
  新增部分索引 `idx_message_deliveries_lease (locked_until) WHERE status = 1`（迁移 `000008`）。

### Changed

- 已发消息列表用一次 LEFT JOIN + GROUP BY 取代每行两个相关子查询。
- 文档：移除「HS256 单密钥不支持轮换」的过期说法（`kid` 密钥环早已落地）。

## [0.1.0] - 模板首个可用版本

### Added

- 模块化单体骨架：`cmd → app → {web, auth, user, dict, message, platform/*}`，包内
  handler → service → repository 分层，`platform/*` 为叶子包。
- 认证与权限：JWT access token + 轮换式 refresh cookie（滑动 30 天 / 绝对上限 90 天）、
  RBAC（admin/member）、登录锁定与同形失败（防邮箱枚举）、改密按用户限流、`kid` 密钥环轮换、
  可选 5s 认证校验缓存；`/api/v1` 默认全部要求登录，401/403 严格区分。
- 统一响应信封 + 稳定业务错误码（`apperr`/`httpx`），zap 结构化日志 + request-id。
- 用户 / 字典（setting + dict array）/ 消息（站内信 + 模板 + 邮件/短信/Webhook outbox worker）
  三个业务域，含保留期清理 worker。
- 可观测性：`/healthz`、`/readyz`、`/metrics`（HTTP + 业务指标）、routekit 生成的 OpenAPI 3.0.3 黄金文件。
- 安全基线：默认不信任代理、每 IP/每用户限流（可切 Redis 共享桶）、安全响应头 + `no-store`、
  请求体/请求头上限、Webhook 拨号时私网拦截、生产密钥占位值校验。
- 工程化：Makefile 质量门（`check`/`audit`/`smoke`/`verify-auth`）、golang-migrate 迁移、
  Docker 多阶段构建 + compose（postgres/redis/migrate/app）、Vue 3 + TS Admin 前端。
