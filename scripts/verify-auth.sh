#!/usr/bin/env bash
# 认证端到端验证：真实进程 + 真实 PostgreSQL（不是内存 SQLite 的集成测试）。
#
# 覆盖 make smoke 与 unit/integration 测试都没覆盖的新行为：
#   A. refresh_reuse_interval：轮换后立刻重放旧 cookie → 200（多标签页竞态），
#      且只保留 1 个活跃 token，session_started_at 跨轮换不变。
#   B. 登出立即生效：登出后同一 cookie 重放 → 401（宽限期不得复活会话）。
#   C. 非浏览器传输：X-Refresh-Transport: body / X-Refresh-Token，不依赖 cookie。
#   D. 会话绝对上限：expires_at 被夹到 session_started_at+max_lifetime。
#   E. 上限的防御分支：配置收紧后旧 token 未被夹紧 → 401 session expired +
#      revoked_reason=session_cap（覆盖 config 变更/历史数据）。
#
# 前置：postgres 已起且迁移已应用（make db-up && make migrate-up）。
# 用法: ./scripts/verify-auth.sh        （= make verify-auth）
# 可调: PORT=18085 ./scripts/verify-auth.sh
# 副作用：创建并删除 verify-auth-*@example.com 的测试用户，不碰其它数据。
set -uo pipefail

cd "$(dirname "$0")/.."

PORT="${PORT:-18085}"
BASE="http://127.0.0.1:${PORT}"
PW='verify-auth-password'
TMPDIR_V="$(mktemp -d)"
BIN="$TMPDIR_V/server"
ADMIN_BIN="$TMPDIR_V/admin"
LOG="$TMPDIR_V/server.log"
JAR="$TMPDIR_V/cookies"
PID=""

# 本机 shell 可能带代理变量：curl 一律 --noproxy '*'，否则打 127.0.0.1 会挂住。
code() { curl -s --noproxy '*' -o /dev/null -w '%{http_code}' "$@"; }
body() { curl -s --noproxy '*' "$@"; }
pg() { docker compose -f deploy/docker-compose.yml exec -T postgres psql -U postgres -d go_web_template -tAc "$1"; }
json_field() { sed -n "s/.*\"$1\":\"\\([^\"]*\\)\".*/\\1/p"; }

stop() {
	[ -n "$PID" ] || return 0
	kill "$PID" 2>/dev/null
	wait "$PID" 2>/dev/null
	PID=""
	for _ in $(seq 40); do [ -z "$(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null)" ] && return 0; sleep 0.1; done
}
cleanup() {
	stop
	pg "delete from refresh_tokens where user_id in (select id from users where email like 'verify-auth-%@example.com')" >/dev/null 2>&1
	pg "delete from users where email like 'verify-auth-%@example.com'" >/dev/null 2>&1
	rm -rf "$TMPDIR_V"
}
trap cleanup EXIT

fail() { echo "verify-auth: FAIL — $*" >&2; echo "---- server log (tail 20) ----" >&2; tail -20 "$LOG" >&2 || true; exit 1; }
pass() { printf '  ok  %s\n' "$*"; }

start() { # $1.. = 额外 env 覆盖
	env APP_LOG_FORMAT=console APP_PORT="$PORT" APP_AUTH_COOKIE_SECURE=false "$@" \
		nohup "$BIN" -config configs/config.yaml >"$LOG" 2>&1 &
	PID=$!
	for _ in $(seq 100); do
		[ "$(code "$BASE/healthz")" = "200" ] && return 0
		kill -0 "$PID" 2>/dev/null || break
		sleep 0.2
	done
	fail "服务未在规定时间内就绪（DB 不可达？先 make db-up && make migrate-up）"
}

seed() { # $1 = email
	APP_SEED_PASSWORD="$PW" "$ADMIN_BIN" -config configs/config.yaml -email "$1" -name "Verify Auth" >/dev/null \
		|| fail "seed 用户失败"
}

login_cookie() { # $1 = email → 存 cookie 到 $JAR，回显 access token
	rm -f "$JAR"
	local out
	out=$(body -c "$JAR" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
		-d "$(printf '{"email":"%s","password":"%s"}' "$1" "$PW")")
	printf '%s' "$out" | grep -q access_token || fail "登录失败: $out"
	printf '%s' "$out" | json_field access_token
}

echo "== 构建"
go build -o "$BIN" ./cmd/server || fail "build server"
go build -o "$ADMIN_BIN" ./cmd/admin || fail "build admin"

echo "== A/B/C: 滑动续期 + 复用宽限期 + 非浏览器传输"
start
EMAIL_A="verify-auth-a-$(date +%s)@example.com"
seed "$EMAIL_A"
TOKEN=$(login_cookie "$EMAIL_A")
COOKIE=$(awk '$6=="gw_refresh"{print $7}' "$JAR")
[ -n "$COOKIE" ] || fail "登录未下发 refresh cookie"

[ "$(code -b "$JAR" -c "$JAR" -X POST "$BASE/api/v1/auth/refresh")" = "200" ] || fail "首次 refresh 应 200"
[ "$(code --cookie "gw_refresh=$COOKIE" -X POST "$BASE/api/v1/auth/refresh")" = "200" ] \
	|| fail "宽限期内重放已轮换的 token 应 200"
pass "轮换后重放旧 cookie → 200（多标签页竞态被容忍）"

UID_A=$(pg "select id from users where email='$EMAIL_A'")
[ "$(pg "select count(*) from refresh_tokens where user_id=$UID_A and revoked_at is null")" = "1" ] \
	|| fail "容忍重放后活跃 token 应只剩 1 个"
[ "$(pg "select count(distinct date_trunc('second',session_started_at)) from refresh_tokens where user_id=$UID_A")" = "1" ] \
	|| fail "session_started_at 应跨轮换保持不变"
pass "只保留 1 个活跃 token，session_started_at 跨轮换一致"

[ "$(code -b "$JAR" -H "Authorization: Bearer $TOKEN" -X POST "$BASE/api/v1/auth/logout")" = "204" ] \
	|| fail "登出应 204（需要 Bearer + refresh cookie）"
LOGGED_OUT_COOKIE=$(awk '$6=="gw_refresh"{print $7}' "$JAR")
[ "$(code --cookie "gw_refresh=$LOGGED_OUT_COOKIE" -X POST "$BASE/api/v1/auth/refresh")" = "401" ] \
	|| fail "登出后重放应 401（宽限期不得复活会话）"
pass "登出立即生效 → 401"

BODY_LOGIN=$(body -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
	-H 'X-Refresh-Transport: body' -d "$(printf '{"email":"%s","password":"%s"}' "$EMAIL_A" "$PW")")
RT1=$(printf '%s' "$BODY_LOGIN" | json_field refresh_token)
[ -n "$RT1" ] || fail "body 传输未返回 refresh_token: $BODY_LOGIN"
RT2=$(body -X POST "$BASE/api/v1/auth/refresh" -H "X-Refresh-Token: $RT1" | json_field refresh_token)
[ -n "$RT2" ] && [ "$RT2" != "$RT1" ] || fail "X-Refresh-Token 刷新未轮换"
pass "X-Refresh-Transport / X-Refresh-Token 可用且轮换"
stop

echo "== D: 会话绝对上限（ttl=3s, max_lifetime=3s → expires_at 被夹紧）"
start APP_AUTH_ACCESS_TTL=1s APP_AUTH_REFRESH_TTL=3s APP_AUTH_REFRESH_MAX_LIFETIME=3s
EMAIL_D="verify-auth-d-$(date +%s)@example.com"
seed "$EMAIL_D"
login_cookie "$EMAIL_D" >/dev/null
[ "$(code -b "$JAR" -c "$JAR" -X POST "$BASE/api/v1/auth/refresh")" = "200" ] || fail "上限内 refresh 应 200"
UID_D=$(pg "select id from users where email='$EMAIL_D'")
pg "select bool_and(expires_at <= session_started_at + interval '3 second') from refresh_tokens where user_id=$UID_D" \
	| grep -q '^t$' || fail "expires_at 未被夹到 session_started_at + max_lifetime"
pass "新 token 的 expires_at 不超过会话绝对上限"
sleep 4
[ "$(code -b "$JAR" -X POST "$BASE/api/v1/auth/refresh")" = "401" ] || fail "超过绝对上限应 401"
pass "超过绝对上限 → 401"
stop

echo "== E: 上限的防御分支（旧 token 未被夹紧，如配置收紧后）"
start
EMAIL_E="verify-auth-e-$(date +%s)@example.com"
seed "$EMAIL_E"
login_cookie "$EMAIL_E" >/dev/null   # 默认 720h/2160h：expires_at 远在未来
stop
sleep 5   # 超过下面设置的 4s 上限，但旧 token 的 expires_at 仍远在未来
start APP_AUTH_ACCESS_TTL=1s APP_AUTH_REFRESH_TTL=4s APP_AUTH_REFRESH_MAX_LIFETIME=4s
OUT_FILE="$TMPDIR_V/refresh.json"
CODE=$(curl -s --noproxy '*' -b "$JAR" -o "$OUT_FILE" -w '%{http_code}' -X POST "$BASE/api/v1/auth/refresh")
OUT=$(cat "$OUT_FILE")
[ "$CODE" = "401" ] || fail "会话超上限应 401，实际 $CODE: $OUT"
printf '%s' "$OUT" | grep -q 'session expired' || fail "应提示 session expired: $OUT"
UID_E=$(pg "select id from users where email='$EMAIL_E'")
[ "$(pg "select revoked_reason from refresh_tokens where user_id=$UID_E order by id desc limit 1")" = "session_cap" ] \
	|| fail "应按 session_cap 吊销"
pass "会话超上限 → 401 session expired（revoked_reason=session_cap）"
stop

echo "verify-auth: PASS（A 宽限期 / B 登出立即 / C body 传输 / D 夹紧 / E 上限防御分支）"
