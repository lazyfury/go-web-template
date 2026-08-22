---
name: docker-deploy
description: go-web-template 的容器化构建与部署操作：多阶段 Dockerfile、docker-compose 编排（postgres+migrate+app）、镜像健康检查、GitHub Actions CI、以及 macOS 下的网络与路径注意点。
whenToUse: 用户要求构建镜像、起编排、部署、写/改 CI，或排查容器运行问题（如健康检查失败、迁移容器报错）时。
---

# 容器化构建与部署

## 1. 镜像构建

```bash
make docker-build          # docker build -t go-web-template:<version> .
# 带版本信息（注入 main.version/commit）
docker build --build-arg VERSION=v1.0.0 --build-arg COMMIT=$(git rev-parse --short HEAD) -t go-web-template:v1.0.0 .
```

- 多阶段构建：builder（golang:1.26-alpine，CGO_ENABLED=0）→ runtime（alpine:3.21，非 root 用户 `app` uid=10001）。
- 产物 `ENTRYPOINT ["/usr/local/bin/server"]`，镜像内 `configs/config.yaml` 为默认配置，覆盖走 `APP_*` 环境变量。
- `.dockerignore` 已排除 `.git`、`docs`、`.env` 等。

## 2. docker-compose 编排（deploy/docker-compose.yml）

```bash
make docker-up             # 等价：docker compose -f deploy/docker-compose.yml up -d --build
make docker-down           # 停容器（保留 pgdata 数据卷）
make docker-logs           # 跟踪 app 日志
```

- 三个服务：`postgres`（16-alpine + pg_isready 健康检查）、`migrate`（profile `migrate`，一次性迁移）、`app`（依赖 postgres healthy）。
- app 健康检查：容器内 `wget -qO- http://localhost:8080/healthz`（alpine busybox 自带 wget）。
- **路径注意**：compose 内相对路径相对 `deploy/` 解析——卷用 `../migrations:/migrations`。
- **网络注意**：compose 网络内 app/migrate 通过服务名 `postgres` 访问 DB；本机调试裸容器时用 `host.docker.internal`。

## 3. 生产配置建议

- 敏感项（密码等）只走环境变量/密钥管理，**不要**写进 compose 文件或提交 `.env`。
- `APP_ENV=production`、`APP_DEBUG=false`、`APP_LOG_FORMAT=json`、`APP_CORS_ALLOWED_ORIGINS` 收紧为实际域名。
- 迁移由部署流水线单独执行（`docker compose run --rm migrate`），避免应用启动时隐式改库。

## 4. CI（.github/workflows/ci.yml）

- 触发：push main/master 或 PR。
- `test` job：setup-go（版本读 go.mod）→ golangci-lint → go vet → `go test -race ./...` → go build。
- `docker` job：依赖 test，执行 `docker build` 验证镜像可构建。
- 新增 Job 注意：runner 是 Linux，`--network host` + 127.0.0.1 可用（与 macOS 不同）。

## 5. 排查表

| 症状 | 处理 |
|---|---|
| 容器反复重启（healthcheck 失败） | `docker compose logs app`；多半是 DB 连不上（env 是否指向 postgres 服务名）或端口冲突 |
| 迁移容器报错 | 卷路径（`../migrations`）、database URL 的用户/库名、postgres 是否 healthy |
| 镜像启动即退 | `docker run --rm -e APP_LOG_FORMAT=console -it go-web-template:dev` 前台看日志 |
| 构建慢 | 分层缓存：改依赖才动 go mod download 层；源码 COPY 在后 |
