# 排错速查（Troubleshooting）

> 报错 → 处理。**新增踩坑经验请追加到这里**（不要写进 `context.md`：那里只放架构与 ADR）。
> 本机专属差异（是否装 docker、代理地址、工具路径）放 `.agents/local.md`，不入库。

## 1. 环境 / 进程

| 症状 | 原因与处理 |
|---|---|
| `curl` 挂起、无响应直到超时 | shell 有 `all_proxy`/`http_proxy`，本机请求也被代理。加 `--noproxy '*'`（`scripts/smoke.sh` 已内置） |
| 起的服务器进程莫名被杀 / 命令一直不返回 | 在 agent 的普通 bash 调用里用 `&` 起长驻进程，工具会等所有子进程。用 `./scripts/smoke.sh`（自起自停 + trap 清理），或 `nohup ... &` + 记 PID + 显式 kill |
| `make lint` 报 command not found | 工具未安装：`make tools`；确认 `$(go env GOPATH)/bin` 在 `PATH` |
| `make migrate-*` 报 docker 不存在 | 无 docker 环境：`make migrate-tool` 后用本地二进制（Makefile 已自动二选一） |
| `make smoke` 报"进程启动即退出" | 多半是 postgres 不可达：`make db-up`（有 docker 走 compose，否则检查本机 postgres） |
| 改了代码、镜像也重建了，但 `curl :8080` 行为还是旧的 | 早先 `nohup` 起的调试二进制成了孤儿进程占着端口，Docker Desktop 的端口映射被遮住（容器日志里看不到你的请求）。`lsof -nP -iTCP:8080 -sTCP:LISTEN` 确认后 `pkill -f '<binary>'`，再 `docker compose up -d --force-recreate app` |

## 2. 配置

- **YAML 时长必须是字符串**：`"10s"`、`"1h"`；裸数字会被当作纳秒。`APP_*` 环境变量同理。
- **配置失败语义**：显式 `-config path` 指向不存在文件 = fail-fast；不传且默认文件缺失 = 默认值 + 环境变量。
- 新增配置项：`config.go` 加字段 → `loader.go` `setDefaults` 加默认值 → `configs/config.example.yaml` 同步 → 必要时加 Validate 分支。

## 3. 数据库 / 迁移

- **SQL 可移植性**：测试库是 SQLite，不支持 `ILIKE`，用 `LOWER(col) LIKE ?`。
- **migrate 容器连不上库**：compose 网络内用服务名 `postgres`；裸 `docker run` 用 `host.docker.internal`（或 `--network host` + `127.0.0.1`）。
- **`first .: file does not exist`**：compose 卷路径相对 **compose 文件所在目录**解析，应为 `../migrations:/migrations`。
- **重复执行 up 输出 `no change`** = 正常（幂等）。
- **表结构改了但测试不生效**：测试用 `AutoMigrate`，需在调用处传入新 model（`testutil.NewSQLiteDB(t, &user.User{}, &post.Post{})`）；生产 schema 仍以迁移 SQL 为准。

## 4. 代码结构

| 症状 | 原因与处理 |
|---|---|
| `import cycle not allowed`（web ↔ 业务域） | `internal/web` 不得 import 业务域；域用 `RegisterRoutes(rg, h)` 挂载。集成测试放 `internal/app`（`package app_test`），通过 `app.NewWithDB` 复用生产装配 |
| `internal/testutil` 与业务包循环 import | `NewSQLiteDB(t, models ...any)` 显式传 model，`testutil` 不 import 任何域包 |
| 超时中间件"没生效" | `Timeout` 只向 `c.Request.Context()` 注入 deadline；handler 不检查 `ctx.Err()` 就不会中断。DB 查询因 `WithContext(ctx)` 会受影响。硬超时由 http server read/write timeout 兜底 |
| `/metrics` 404 | 不能用 DefaultServeMux 自动注册，直挂 `promhttp.Handler()` |
| `errors.Is(err, apperrors.ErrNotFound)` 不成立 | `apperrors.Error.Is` 按 Code 匹配；确认是 `apperrors.Wrap/New` 产出的错误，且调用了 `Unwrap` 链（`fmt.Errorf("%w")` 也可） |
| `TestOpenAPIGolden` 失败 | 改了路由/`Op` 但没更新契约：运行 `make openapi`，review `docs/openapi.json` 后提交 |
| 启动报 `routekit: unsupported type` / `path parameter ... missing` | `Op` 与 gin path 不一致，或 DTO 含 routekit 未覆盖的类型；按错误中的路由/类型修正（query 数组/对象、通配路径当前不支持） |

## 5. golangci-lint

- **`unsupported version of the configuration: ""`**：配置是 v1 格式而二进制是 v2。本仓库 `.golangci.yml` 已是 v2 格式（`version: "2"`），升级工具时用 `golangci-lint migrate`。
- 常见报错修复：

| 报错 | 修法 |
|---|---|
| `exported: ... should have comment` | 导出标识符补注释（含 const 块内每一项） |
| `hugeParam` | 大结构体参数改传指针 |
| `unnecessaryBlock` | 去掉多余 `{ }` |
| `unnamedResult` | 返回值命名 |
| `prealloc` | `var x []T` → `x := make([]T, 0, n)` |
| `ifElseChain` | 改 `switch` |
| `ineffectual assignment` | 未用变量改 `_` |
| `importShadow` | 参数名不要与 import 包名同名（如 `service service.UserService` → `svc`） |
| `noctx: httptest.NewRequest must not be called` | 改 `httptest.NewRequestWithContext(context.Background(), ...)` |
| `gofmt`/`goimports` 报差异 | `make fmt`（`docs/` 为生成代码，已豁免 `make fmt-check`） |

## 6. 前端（web/admin）

| 报错 | 处理 |
|---|---|
| `TS5103 Invalid value for '--ignoreDeprecations'` | tsconfig 的值必须匹配 TS major：TS 5.x 只认 `"5.0"`，TS 6.0/7.0 认 `"6.0"`（见 `context.md` ADR 11） |
| `[sass] Expected newline` | `<style lang="sass">` 是缩进式语法却写了 SCSS 花括号 → 改 `lang="scss"` |
| `[unocss] failed to load icon "-"` | 无害噪音，来自 JSDoc 注释里独立的 `-` token，可忽略 |

## 7. 容器

| 症状 | 原因与处理 |
|---|---|
| `connection refused` (5432) | postgres 未起 / 网络模式不对（compose 内用服务名，裸容器用 host.docker.internal） |
| `readyz` 503 | DB 不可达；先 `docker compose ps` 看 postgres 状态 |
| 容器反复重启（healthcheck 失败） | `make docker-logs`；多半是 DB 连不上（env 是否指向服务名）或端口冲突 |
| 镜像启动即退 | `docker run --rm -e APP_LOG_FORMAT=console -it go-web-template:dev` 前台看日志 |
| 构建慢 | 分层缓存：依赖层在前，源码 COPY 在后 |
