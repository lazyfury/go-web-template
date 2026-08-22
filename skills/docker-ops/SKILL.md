---
name: docker-ops
description: go-web-template 的容器编排与本地端到端调试：起 postgres→迁移→受管后台任务跑服务→curl（--noproxy）验证健康检查/CRUD/限流→优雅停机；以及镜像构建（多阶段 Dockerfile）、docker-compose 编排（postgres+migrate+app）、GitHub Actions CI、macOS 网络与路径注意点。
whenToUse: 用户要求"本地跑起来看看 / 调 API / 复现排查问题 / 验证改动效果"，或构建镜像、起编排、部署、写/改 CI、排查容器运行问题时。
---

# 容器编排与本地端到端调试

## 0. 环境已知坑（先读，避免浪费时间）

- **curl 必须加 `--noproxy '*'`**：本机 shell 有 `all_proxy=http://127.0.0.1:7890`，不加会挂起超时。
- **服务器必须用受管后台任务运行**：不要在普通 bash 命令里 `&` 起服务（harness 会等全部子进程 → 超时 → SIGTERM 杀掉）。用 `run_in_background: true` 起服务，验证完 `job_kill`。

## 1. 本地端到端调试

### 1.1 起数据库 + 迁移

```bash
docker compose -f deploy/docker-compose.yml up -d postgres     # 等 healthy（pg_isready 探活，最多 ~60s）
docker compose -f deploy/docker-compose.yml run --rm migrate   # 首次 "1/u create_users_table"，重复 "no change"
```

迁移容器访问 postgres 走 compose 网络（服务名 `postgres`）。裸 docker run 调试用 `host.docker.internal` 或 `--network host` + `127.0.0.1`（macOS）。

### 1.2 启动服务（受管后台任务）

```bash
go build -o /tmp/gwt-server ./cmd/server
# 然后以 run_in_background: true 运行：
APP_LOG_FORMAT=console APP_LOG_LEVEL=info /tmp/gwt-server -config configs/config.yaml
```

`APP_LOG_FORMAT=console` 便于读日志；默认配置连 `127.0.0.1:5432`（compose 已映射端口）。改了代码需重新 `go build` 再起新任务（不会热重载）。

### 1.3 探活与冒烟（curl 一律加 --noproxy）

```bash
B=http://127.0.0.1:8080
curl -s --noproxy '*' $B/healthz                    # {"status":"ok"}
curl -s --noproxy '*' $B/readyz                     # {"status":"ok"}（连不上 DB 时 503）
curl -s --noproxy '*' $B/metrics | grep ^http_      # Prometheus 指标
curl -s -o /dev/null -w '%{http_code}' --noproxy '*' $B/swagger/index.html   # 200
# CRUD 冒烟
curl -s --noproxy '*' -X POST $B/api/v1/users -H 'Content-Type: application/json' -d '{"name":"Alice","email":"a@b.com"}'
curl -s --noproxy '*' "$B/api/v1/users?page=1&page_size=10"
curl -s --noproxy '*' $B/api/v1/users/1
curl -s --noproxy '*' -X PATCH $B/api/v1/users/1 -H 'Content-Type: application/json' -d '{"name":"Alicia"}'
curl -s --noproxy '*' -o /dev/null -w '%{http_code}' --noproxy '*' -X DELETE $B/api/v1/users/1   # 204
# 限流演示：重启服务时设 APP_RATELIMIT_RPS=2 APP_RATELIMIT_BURST=2，连打 5 次列表接口 → 前 2 个 200 后 3 个 429
```

预期状态码：创建 201、重复邮箱 409、非法参数 400、不存在 404、删除 204、限流 429、超时 504。优雅停机：`job_kill` 后输出应出现 `shutdown signal received, draining connections` → `application stopped`。

## 2. 镜像构建

```bash
make docker-build          # docker build -t go-web-template:<version> .
# 带版本信息（注入 main.version/commit）
docker build --build-arg VERSION=v1.0.0 --build-arg COMMIT=$(git rev-parse --short HEAD) -t go-web-template:v1.0.0 .
```

- 多阶段构建：builder（golang:1.26-alpine，CGO_ENABLED=0）→ runtime（alpine:3.21，非 root 用户 `app` uid=10001）。
- 产物 `ENTRYPOINT ["/usr/local/bin/server"]`，镜像内 `configs/config.yaml` 为默认配置，覆盖走 `APP_*` 环境变量。
- `.dockerignore` 已排除 `.git`、`docs`、`.env` 等。

## 3. docker-compose 编排（deploy/docker-compose.yml）

```bash
make docker-up             # 等价：docker compose -f deploy/docker-compose.yml up -d --build
make docker-down           # 停容器（保留 pgdata 数据卷）
make docker-logs           # 跟踪 app 日志
```

- 三个服务：`postgres`（16-alpine + pg_isready 健康检查）、`migrate`（profile `migrate`，一次性迁移）、`app`（依赖 postgres healthy）。
- app 健康检查：容器内 `wget -qO- http://localhost:8080/healthz`（alpine busybox 自带 wget）。
- **路径注意**：compose 内相对路径相对 `deploy/` 解析——卷用 `../migrations:/migrations`。
- **网络注意**：compose 网络内 app/migrate 通过服务名 `postgres` 访问 DB；本机调试裸容器时用 `host.docker.internal`。

## 4. CI（.github/workflows/ci.yml）

- 触发：push main/master 或 PR。
- `test` job：setup-go（版本读 go.mod）→ golangci-lint → go vet → `go test -race ./...` → go build。
- `docker` job：依赖 test，执行 `docker build` 验证镜像可构建。
- 新增 Job 注意：runner 是 Linux，`--network host` + 127.0.0.1 可用（与 macOS 不同）。

## 5. 生产配置建议

- 敏感项（密码等）只走环境变量/密钥管理，**不要**写进 compose 文件或提交 `.env`。
- `APP_ENV=production`、`APP_DEBUG=false`、`APP_LOG_FORMAT=json`、`APP_CORS_ALLOWED_ORIGINS` 收紧为实际域名。
- 迁移由部署流水线单独执行（`docker compose run --rm migrate`），避免应用启动时隐式改库。

## 6. 排查表

| 症状 | 原因与处理 |
|---|---|
| curl 挂起超时 | 忘加 `--noproxy '*'` |
| `connection refused` (5432) | postgres 未起 / 网络模式不对（compose 内用服务名，裸容器用 host.docker.internal） |
| 迁移报 `first .: file does not exist` | compose 卷路径写错（相对 compose 文件目录，应为 `../migrations`） |
| `/metrics` 404 | 代码里用了 DefaultServeMux；应 `promhttp.Handler()` |
| readyz 503 | DB 不可达；先查 postgres 容器状态 |
| 启动报 config 错误 | 显式 `-config` 文件不存在会 fail-fast；检查路径或改用默认加载 |
| 容器反复重启（healthcheck 失败） | `docker compose logs app`；多半是 DB 连不上（env 是否指向 postgres 服务名）或端口冲突 |
| 镜像启动即退 | `docker run --rm -e APP_LOG_FORMAT=console -it go-web-template:dev` 前台看日志 |
| 构建慢 | 分层缓存：改依赖才动 go mod download 层；源码 COPY 在后 |

## 7. 收尾

- `job_kill` 停服务；`docker compose -f deploy/docker-compose.yml down` 停容器（保留数据卷）。
