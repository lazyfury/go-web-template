---
name: debug-e2e
description: 在本机对 go-web-template 做端到端调试：起 postgres → 迁移 → 以受管后台任务运行服务 → 用 curl（--noproxy）验证 healthz/readyz/metrics/swagger/CRUD/限流 → 优雅停机。包含本机环境的已知坑与排查表。
whenToUse: 用户要求"本地跑起来看看 / 调 API / 复现并排查问题 / 验证改动效果"时。
---

# 本地端到端调试

## 0. 环境已知坑（先读，避免浪费时间）

- **curl 必须加 `--noproxy '*'`**：本机 shell 有 `all_proxy=http://127.0.0.1:7890`，不加会挂起超时。
- **服务器必须用受管后台任务运行**：不要在普通 bash 命令里 `&` 起服务（harness 会等全部子进程 → 超时 → SIGTERM 杀掉）。用 `run_in_background: true` 起服务，验证完 `job_kill`。

## 1. 起数据库（Docker）

```bash
docker compose -f deploy/docker-compose.yml up -d postgres
# 等待 healthy（pg_isready 探活），最多 ~60s
```

## 2. 执行迁移

```bash
docker compose -f deploy/docker-compose.yml run --rm migrate
# 首次输出 "1/u create_users_table"，重复执行输出 "no change"
```

迁移容器访问 postgres 走 compose 网络（服务名 `postgres`）。若需裸 docker run 调试：用 `host.docker.internal` 或 `--network host` + `127.0.0.1`（macOS）。

## 3. 启动服务（受管后台任务）

```bash
# 先编译
go build -o /tmp/gwt-server ./cmd/server
# 然后以 run_in_background: true 运行：
APP_LOG_FORMAT=console APP_LOG_LEVEL=info /tmp/gwt-server -config configs/config.yaml
```

`APP_LOG_FORMAT=console` 便于读日志。默认配置连 `127.0.0.1:5432`（compose 已映射端口）。

## 4. 探活与冒烟（curl 一律加 --noproxy）

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

预期状态码：创建 201、重复邮箱 409、非法参数 400、不存在 404、删除 204、限流 429、超时 504。

## 5. 优雅停机验证

`job_kill` 服务任务后，查看其输出应出现 `shutdown signal received, draining connections` → `application stopped`。

## 6. 排查表

| 症状 | 原因与处理 |
|---|---|
| curl 挂起超时 | 忘加 `--noproxy '*'` |
| `connection refused` (5432) | postgres 未起 / 网络模式不对（compose 内用服务名，裸容器用 host.docker.internal） |
| 迁移报 `first .: file does not exist` | compose 卷路径写错（相对 compose 文件目录，应为 `../migrations`） |
| `/metrics` 404 | 代码里用了 DefaultServeMux；应 `promhttp.Handler()` |
| readyz 503 | DB 不可达；先查 postgres 容器状态 |
| 启动报 config 错误 | 显式 `-config` 文件不存在会 fail-fast；检查路径或改用默认加载 |

## 7. 收尾

- `job_kill` 停服务；`docker compose -f deploy/docker-compose.yml down` 停容器（保留数据卷）。
- 调试中若改了代码，重新 `go build` 后再起新任务（受管任务不会热重载）。
