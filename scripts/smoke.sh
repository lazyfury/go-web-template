#!/usr/bin/env bash
# 本地冒烟测试：自起自停一个真实进程，验证探针、登录/JWT 鉴权与 users CRUD。
#
# 设计要点（harness 无关，不依赖任何 agent 后台任务能力）:
#   - 本脚本自己 nohup 起服务、trap 清理，任何 agent/人类都能直接跑。
#   - curl 一律 --noproxy '*'（本机 shell 可能带 all_proxy，否则会挂起）。
#   - DB 不可用时（/readyz 503）自动降级为「只验 liveness」，并明确提示 SKIP。
#   - DB 可用时会 seed 一个管理员并完整走 login → bearer → CRUD → 401 全链路。
#
# 用法: ./scripts/smoke.sh        （= make smoke）
# 可调: PORT=18080 TIMEOUT=40 ./scripts/smoke.sh
set -euo pipefail

cd "$(dirname "$0")/.."

PORT="${PORT:-18080}"
TIMEOUT="${TIMEOUT:-30}"
BIN="${BIN:-/tmp/go-web-template-smoke}"
ADMIN_BIN="${ADMIN_BIN:-/tmp/go-web-template-smoke-admin}"
LOG="${LOG:-/tmp/go-web-template-smoke.log}"
BASE="http://127.0.0.1:${PORT}"

SMOKE_EMAIL="smoke-admin@example.com"
SMOKE_PASSWORD="smoke-admin-password"
JAR="${JAR:-/tmp/go-web-template-smoke-cookies}"

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
go build -o "$ADMIN_BIN" ./cmd/admin

echo "==> starting server on :$PORT (log: $LOG)"
APP_LOG_FORMAT=console APP_PORT="$PORT" APP_AUTH_COOKIE_SECURE=false \
  nohup "$BIN" -config configs/config.yaml >"$LOG" 2>&1 &
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
  echo "smoke: PASS（仅 liveness；/readyz 非 200，postgres 不可用 → 鉴权/CRUD 跳过）"
  echo "  提示: 起库并迁移后重跑本脚本可覆盖「登录 + 鉴权 + CRUD」全链路"
  exit 0
fi

# 3) 匿名访问必须 401
[ "$(code "$BASE/api/v1/users")" = "401" ] || fail "匿名 GET /users 应为 401"
[ "$(code -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' -d '{}')" = "400" ] \
  || fail "空登录体应为 400"

# 4) seed 管理员 + 登录（需要 schema 已迁移）
echo "==> seeding administrator"
APP_SEED_PASSWORD="$SMOKE_PASSWORD" "$ADMIN_BIN" -config configs/config.yaml \
  -email "$SMOKE_EMAIL" -name "Smoke Admin" >/dev/null \
  || fail "seed 管理员失败（多半是未执行迁移：make migrate-up）"

# JSON 载荷先用变量拼好再用 -d "$VAR" 传入：避免在 "$( )" 里嵌套转义引号
# 被二次解析（会导致服务端收到一个 JSON 字符串而不是对象）。
LOGIN_PAYLOAD=$(printf '{"email":"%s","password":"%s"}' "$SMOKE_EMAIL" "$SMOKE_PASSWORD")
WRONG_PAYLOAD=$(printf '{"email":"%s","password":"%s"}' "$SMOKE_EMAIL" "wrong-password")

rm -f "$JAR"
LOGIN_BODY=$(body -c "$JAR" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' -d "$LOGIN_PAYLOAD")
TOKEN=$(printf '%s' "$LOGIN_BODY" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p' | head -1)
[ -n "$TOKEN" ] || fail "登录未返回 access_token: $LOGIN_BODY"
AUTH=(-H "Authorization: Bearer $TOKEN")

[ "$(code -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' -d "$WRONG_PAYLOAD")" = "401" ] \
  || fail "错误密码应为 401（且不泄露用户是否存在）"
[ "$(code "${AUTH[@]}" "$BASE/api/v1/auth/me")" = "200" ] || fail "带 token 的 /auth/me 非 200"

# 5) users CRUD（全部需要登录）
EMAIL="smoke-$(date +%s)@example.com"
CREATE_PAYLOAD=$(printf '{"name":"Smoke","email":"%s","password":"smoke-user-password"}' "$EMAIL")
CREATE_BODY=$(body "${AUTH[@]}" -X POST "$BASE/api/v1/users" -H 'Content-Type: application/json' -d "$CREATE_PAYLOAD")
ID=$(printf '%s' "$CREATE_BODY" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -1)
[ -n "$ID" ] || fail "创建用户未返回 id: $CREATE_BODY"

[ "$(code "${AUTH[@]}" "$BASE/api/v1/users?page=1&page_size=10")" = "200" ] || fail "列表接口非 200"
[ "$(code "${AUTH[@]}" "$BASE/api/v1/users/$ID")" = "200" ] || fail "详情接口非 200 (id=$ID)"
[ "$(code "${AUTH[@]}" -X PATCH "$BASE/api/v1/users/$ID" -H 'Content-Type: application/json' -d '{"name":"SmokeRenamed"}')" = "200" ] \
  || fail "PATCH 非 200 (id=$ID)"
[ "$(code "${AUTH[@]}" -X DELETE "$BASE/api/v1/users/$ID")" = "204" ] || fail "DELETE 非 204 (id=$ID)"
[ "$(code "${AUTH[@]}" "$BASE/api/v1/users/$ID")" = "404" ] || fail "软删后详情应为 404 (id=$ID)"
[ "$(code "${AUTH[@]}" -X POST "$BASE/api/v1/users" -H 'Content-Type: application/json' -d '{"name":"x"}')" = "400" ] \
  || fail "非法参数应为 400"

# 6) refresh cookie 轮换：jar 里应能换到新 token
REFRESH_BODY=$(body -b "$JAR" -c "$JAR" -X POST "$BASE/api/v1/auth/refresh")
printf '%s' "$REFRESH_BODY" | grep -q '"access_token"' || fail "refresh 未返回 access_token: $REFRESH_BODY"
[ "$(body -b "$JAR" -X POST "$BASE/api/v1/auth/logout" -H "Authorization: Bearer $TOKEN" -o /dev/null -w '%{http_code}')" = "204" ] \
  || fail "logout 非 204"

# 7) 文档端点
[ "$(code "$BASE/swagger/index.html")" = "200" ] || fail "swagger UI 非 200"
[ "$(code "$BASE/openapi.json")" = "200" ] || fail "openapi.json 非 200"

rm -f "$JAR"
echo "smoke: PASS（liveness + readyz + 登录/JWT + 权限拒绝 + CRUD + refresh + OpenAPI/Swagger）"
