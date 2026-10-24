#!/usr/bin/env bash
# 本地冒烟测试：自起自停一个真实进程，验证探针与 users CRUD。
#
# 设计要点（harness 无关，不依赖任何 agent 后台任务能力）:
#   - 本脚本自己 nohup 起服务、trap 清理，任何 agent/人类都能直接跑。
#   - curl 一律 --noproxy '*'（本机 shell 可能带 all_proxy，否则会挂起）。
#   - DB 不可用时（/readyz 503）自动降级为「只验 liveness」，并明确提示 SKIP。
#
# 用法: ./scripts/smoke.sh        （= make smoke）
# 可调: PORT=18080 TIMEOUT=40 ./scripts/smoke.sh
set -euo pipefail

cd "$(dirname "$0")/.."

PORT="${PORT:-18080}"
TIMEOUT="${TIMEOUT:-30}"
BIN="${BIN:-/tmp/go-web-template-smoke}"
LOG="${LOG:-/tmp/go-web-template-smoke.log}"
BASE="http://127.0.0.1:${PORT}"

fail() {
  echo "smoke: FAIL — $*" >&2
  echo "---- server log (tail 40) ----" >&2
  tail -40 "$LOG" >&2 || true
  exit 1
}

code() { curl -s --noproxy '*' -o /dev/null -w '%{http_code}' "$@"; }
body() { curl -s --noproxy '*' "$@"; }

echo "==> building $BIN"
go build -o "$BIN" ./cmd/server

echo "==> starting server on :$PORT (log: $LOG)"
APP_LOG_FORMAT=console APP_PORT="$PORT" nohup "$BIN" -config configs/config.yaml >"$LOG" 2>&1 &
PID=$!
trap 'kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true' EXIT

# 1) 等进程起来（liveness 与 DB 无关）
for _ in $(seq "$((TIMEOUT * 2))"); do
  if ! kill -0 "$PID" 2>/dev/null; then
    fail "进程启动即退出（多半是 postgres 不可达：先 make db-up 或 docker compose up -d postgres）"
  fi
  if [ "$(code "$BASE/healthz")" = "200" ]; then break; fi
  sleep 0.5
done
[ "$(code "$BASE/healthz")" = "200" ] || fail "healthz 未在 ${TIMEOUT}s 内就绪"

# 2) readiness：DB 不通就只报 liveness
if [ "$(code "$BASE/readyz")" != "200" ]; then
  echo "smoke: PASS（仅 liveness；/readyz 非 200，postgres 不可用 → CRUD 跳过）"
  echo "  提示: 起库后重跑本脚本可覆盖 CRUD 全链路"
  exit 0
fi

# 3) users CRUD 全链路
EMAIL="smoke-$(date +%s)@example.com"
CREATE_BODY=$(body -X POST "$BASE/api/v1/users" -H 'Content-Type: application/json' \
  -d "{\"name\":\"Smoke\",\"email\":\"$EMAIL\"}")
ID=$(printf '%s' "$CREATE_BODY" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -1)
[ -n "$ID" ] || fail "创建用户未返回 id: $CREATE_BODY"

[ "$(code "$BASE/api/v1/users?page=1&page_size=10")" = "200" ] || fail "列表接口非 200"
[ "$(code "$BASE/api/v1/users/$ID")" = "200" ] || fail "详情接口非 200 (id=$ID)"
[ "$(code -X PATCH "$BASE/api/v1/users/$ID" -H 'Content-Type: application/json' -d '{"name":"SmokeRenamed"}')" = "200" ] \
  || fail "PATCH 非 200 (id=$ID)"
[ "$(code -X DELETE "$BASE/api/v1/users/$ID")" = "204" ] || fail "DELETE 非 204 (id=$ID)"
[ "$(code "$BASE/api/v1/users/$ID")" = "404" ] || fail "软删后详情应为 404 (id=$ID)"
[ "$(code -X POST "$BASE/api/v1/users" -H 'Content-Type: application/json' -d '{"name":"x"}')" = "400" ] \
  || fail "非法参数应为 400"
[ "$(code "$BASE/swagger/index.html")" = "200" ] || fail "swagger UI 非 200"
[ "$(code "$BASE/openapi.json")" = "200" ] || fail "openapi.json 非 200"

echo "smoke: PASS（liveness + readyz + CRUD + 校验 + OpenAPI/Swagger）"
