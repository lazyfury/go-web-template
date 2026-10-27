# Changelog

本仓库是模板（template），版本号用 `vMAJOR.MINOR.PATCH` 打 tag；`Unreleased` 段记录尚未发版的改动。
格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，事实以 `git log` 为准。

## [Unreleased]

### Added

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
