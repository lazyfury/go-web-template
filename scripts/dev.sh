#!/usr/bin/env bash
# 一键本地开发：起 postgres → 迁移 → 热重载运行
# 用法: ./scripts/dev.sh   （或 make dev 之后单独执行本脚本的依赖部分）
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> 启动 postgres (docker compose)"
docker compose -f deploy/docker-compose.yml up -d postgres

echo "==> 等待 postgres 就绪"
until docker compose -f deploy/docker-compose.yml exec postgres pg_isready -U postgres -d go_web_template >/dev/null 2>&1; do
  sleep 1
done

echo "==> 执行数据库迁移"
docker compose -f deploy/docker-compose.yml run --rm migrate

echo "==> 启动应用（Ctrl+C 停止）"
exec go run ./cmd/server "$@"
