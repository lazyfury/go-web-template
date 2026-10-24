---
name: docker-ops
description: go-web-template 的容器编排与本地端到端调试：起 postgres → 迁移 → 启动服务 → 验证健康检查/CRUD/限流 → 优雅停机；以及镜像构建（多阶段 Dockerfile）、docker-compose 编排（postgres+migrate+app）、GitHub Actions CI。用于"本地跑起来看看 / 调 API / 复现问题 / 验证改动 / 构建镜像 / 部署 / 排查容器问题"。
---

# 容器编排与本地端到端调试

## 0. 两个前提（先看，避免浪费时间）

- **长驻进程不要用 agent 的普通 bash 调用 `&` 起**：工具会等所有子进程 → 超时 → SIGTERM。
  用仓库自带脚本（自起自停 + trap 清理 + `--noproxy`）：`make smoke`（源码见 `scripts/smoke.sh`）。
- **本机是否有 docker**：看 `.agents/local.md`。没有 docker 时，`make db-up` / `make migrate-*` 会自动回退到本机 postgres + 本地 `migrate` 二进制；`make smoke` 在无 DB 时只验证 liveness。

## 1. 一条命令冒烟（首选）

```bash
make db-up            # 起 postgres 并等 healthy（有 docker 走 compose）
make migrate-up       # 应用迁移（首次 "1/u create_users_table"，重复 "no change"）
make smoke            # 起服务 → /healthz /readyz → users CRUD → 软删 404 → 校验 400 → OpenAPI/Swagger
make docker-down      # 收尾停容器（保留数据卷）；本机服务由 smoke 自己清理
```

`make smoke` 的断言：创建 201、列表/详情 200、PATCH 200、DELETE 204、软删后 404、非法参数 400、`/openapi.json` 200、`/swagger/index.html` 200。失败时打印 server 日志尾部与修复提示。

## 2. 手工调试（需要看细节时）

```bash
go build -o /tmp/gwt-server ./cmd/server
APP_LOG_FORMAT=console APP_PORT=18080 nohup /tmp/gwt-server -config configs/config.yaml >/tmp/gwt.log 2>&1 &
echo $! > /tmp/gwt.pid
# ...验证...
kill "$(cat /tmp/gwt.pid)"        # 日志应出现 "shutdown signal received, draining connections" → "application stopped"
```

```bash
B=http://127.0.0.1:18080
curl -s --noproxy '*' $B/healthz
curl -s --noproxy '*' $B/readyz                       # DB 不可达时 503
curl -s --noproxy '*' $B/metrics | grep ^http_
curl -s --noproxy '*' -X POST $B/api/v1/users -H 'Content-Type: application/json' \
  -d '{"name":"Alice","email":"a@b.com"}'
# 限流：APP_RATELIMIT_RPS=2 APP_RATELIMIT_BURST=2 重启后连打 5 次列表 → 200,200,429,429,429
```

预期状态码：201 / 409（重复邮箱）/ 400（非法参数）/ 404（不存在）/ 204（删除）/ 429（限流）/ 504（超时）。

## 3. 镜像构建

```bash
make docker-build
docker build --build-arg VERSION=v1.0.0 --build-arg COMMIT=$(git rev-parse --short HEAD) -t go-web-template:v1.0.0 .
```

- 多阶段：builder（golang:1.26-alpine，CGO_ENABLED=0）→ runtime（alpine:3.21，非 root `app` uid=10001）。
- 镜像内 `configs/config.yaml` 为默认配置，覆盖走 `APP_*` 环境变量。
- `.dockerignore` 已排除 `.git`、`docs`、`.env` 等。

## 4. compose 编排（`deploy/docker-compose.yml`）

```bash
make docker-up     # postgres + migrate + app（等价 docker compose -f deploy/docker-compose.yml up -d --build）
make docker-logs   # 跟踪 app 日志
make docker-down   # 停容器（保留 pgdata 卷；加 -v 会删数据，属破坏性操作）
```

- 三服务：`postgres`（16-alpine + pg_isready 健康检查）、`migrate`（一次性迁移）、`app`（依赖 postgres healthy）。
- app 健康检查：容器内 `wget -qO- http://localhost:8080/healthz`。
- **相对路径相对 `deploy/` 解析**：卷写 `../migrations:/migrations`。
- **网络**：compose 内用服务名 `postgres`；裸容器用 `host.docker.internal`，或 `--network host` + `127.0.0.1`。

## 5. CI（`.github/workflows/ci.yml`）

- 触发：push main/master 或 PR。工具版本 pin 在 workflow `env`，须与 `Makefile` 顶部一致。
- `lint` job：golangci-lint（v2 配置，pin 版本）。
- `gate` job：`make gate`（gofmt 校验 + vet + race 测试 + build + `docs/` 同步检查）。
- `web` job：`pnpm install --frozen-lockfile` + `typecheck` + `build`。
- `docker` job：依赖 gate/web，执行 `docker build`。
- Linux runner 上 `--network host` + `127.0.0.1` 可用（与 macOS 不同）。

## 6. 生产配置建议

- 敏感项只走环境变量/密钥管理，**不要**写进 compose 或提交 `.env`。
- `APP_ENV=production`、`APP_DEBUG=false`、`APP_LOG_FORMAT=json`、CORS 收紧为实际域名。
- 迁移由部署流水线单独执行（`docker compose run --rm migrate`），不在应用启动时隐式改库。

## 7. 排查

容器起不来、端口冲突、connection refused、迁移卷路径、`/metrics` 404 等 → 见 **`docs/troubleshooting.md`**（保持单一事实来源，不在此重复）。
