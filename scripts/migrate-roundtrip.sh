#!/usr/bin/env bash
# 迁移可逆性验证（改迁移 SQL / 发布前的门）：
#   在一个独立的 scratch 库上跑 up → down 全部 → 再 up，并断言版本号与核心表。
#
# 为什么需要：audit.sh 只检查 up/down 文件成对且 down 非空，真正的 down SQL 从未被执行过；
# 一次「up 能跑、down 炸」的迁移只会在回滚当天才被发现。
#
# 不碰开发者正在用的库：MIGRATE_URL 只用来推导 scratch 库名，所有语句都跑在
# <dbname>_roundtrip 上，结束时 DROP。psql 通过 compose 的 postgres 容器执行，
# 因此要求 `make db-up` 已经起好本地 postgres（脚本不自动启动容器）。
#
# 用法:
#   make db-up && make migrate-roundtrip
# 可调:
#   MIGRATE_URL=postgres://postgres:postgres@127.0.0.1:5432/go_web_template?sslmode=disable
#   SCRATCH_SUFFIX=_roundtrip
set -uo pipefail

cd "$(dirname "$0")/.."

COMPOSE_FILE=${COMPOSE_FILE:-deploy/docker-compose.yml}
MIGRATE_URL=${MIGRATE_URL:-postgres://postgres:postgres@127.0.0.1:5432/go_web_template?sslmode=disable}
SCRATCH_SUFFIX=${SCRATCH_SUFFIX:-_roundtrip}

info() { printf '==> %s\n' "$1"; }
fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }

# --- 1. 前提检查 -------------------------------------------------------------

case "$MIGRATE_URL" in
*@127.0.0.1:* | *@localhost:*) ;;
*) fail "只支持本地 compose postgres（MIGRATE_URL 指向 127.0.0.1/localhost）；远程库请用独立实例验证" ;;
esac

if ! docker compose -f "$COMPOSE_FILE" ps -q postgres >/dev/null 2>&1 ||
	[ -z "$(docker compose -f "$COMPOSE_FILE" ps -q postgres)" ]; then
	fail "postgres 容器未运行：先 make db-up"
fi

# 管理语句（CREATE/DROP DATABASE）连 postgres；断言必须连 scratch 库。
psql_admin() {
	docker compose -f "$COMPOSE_FILE" exec -T postgres \
		psql -U postgres -d postgres -v ON_ERROR_STOP=1 -tAc "$1"
}
psql_scratch() {
	docker compose -f "$COMPOSE_FILE" exec -T postgres \
		psql -U postgres -d "$scratch" -v ON_ERROR_STOP=1 -tAc "$1"
}

dbname=$(printf '%s' "$MIGRATE_URL" | sed -E 's#^[a-z]+://[^/]+/([^?]+).*#\1#')
[ -n "$dbname" ] && [ "$dbname" != "$MIGRATE_URL" ] || fail "无法从 MIGRATE_URL 解析库名"
scratch="${dbname}${SCRATCH_SUFFIX}"
[ "$scratch" != "$dbname" ] || fail "scratch 库名必须带后缀，拒绝直接操作 $dbname"
scratch_url=$(printf '%s' "$MIGRATE_URL" | sed -E "s#(://[^/]+/)[^?]+#\\1${scratch}#")

required=$(ls migrations/*.up.sql 2>/dev/null | sed -E 's#.*/([0-9]+)_.*#\1#' | sort -n | tail -1)
[ -n "$required" ] || fail "migrations/ 下没有 up 迁移"
required=$((10#$required)) # 文件名 000008 → schema_migrations 里的 8

cleanup() { psql_admin "DROP DATABASE IF EXISTS \"$scratch\"" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# --- 2. 迁移执行器 -----------------------------------------------------------

migrate_run() {
	if [ -x "$(go env GOPATH)/bin/migrate" ]; then
		"$(go env GOPATH)/bin/migrate" -path migrations -database "$scratch_url" "$@"
	elif command -v migrate >/dev/null 2>&1; then
		migrate -path migrations -database "$scratch_url" "$@"
	else
		docker run --rm -v "$PWD/migrations:/migrations" --network host migrate/migrate \
			-path=/migrations -database "$scratch_url" "$@"
	fi
}

applied_version() { psql_scratch "SELECT version FROM schema_migrations" 2>/dev/null; }
assert_version() {
	local want=$1 got
	got=$(applied_version)
	[ "$got" = "$want" ] || fail "schema_migrations.version = ${got:-<none>}, want ${want}（步骤: $2）"
}

# --- 3. 三轮：up → down all → up -------------------------------------------

info "scratch 库: ${scratch}（required=${required}）"
psql_admin "DROP DATABASE IF EXISTS \"$scratch\"" >/dev/null || fail "清理旧 scratch 库失败"
psql_admin "CREATE DATABASE \"$scratch\"" >/dev/null || fail "创建 scratch 库失败"

info "1/3 up"
migrate_run up >/dev/null || fail "migrate up 失败"
assert_version "$required" "up"
[ "$(psql_scratch "SELECT to_regclass('public.users') IS NOT NULL")" = "t" ] || fail "up 后 users 表不存在"

info "2/3 down -all"
migrate_run down -all >/dev/null || fail "migrate down -all 失败（down SQL 不可逆）"
# down 000001 会 DROP users；任何一张核心表残留都说明 down 链断在某一步。
for table in users refresh_tokens dict_groups messages message_templates; do
	if [ "$(psql_scratch "SELECT to_regclass('public.$table') IS NOT NULL")" = "t" ]; then
		fail "down -all 后仍存在表 ${table}（对应的 down 迁移没有真正回滚）"
	fi
done

info "3/3 up（回滚后重建）"
migrate_run up >/dev/null || fail "down -all 之后重新 up 失败"
assert_version "$required" "re-up"

echo "migrate-roundtrip: ok（up → down all → up，版本 ${required}）"
