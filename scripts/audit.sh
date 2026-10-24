#!/usr/bin/env bash
# 安全 / SQL 静态审计 —— 只做「可机械判定」的部分。
#
# 为什么单独一个目标而不并入 make check：
#   - govulncheck 需要拉 vuln DB（慢 + 依赖网络），不适合每次提交；
#   - 这些检查的触发点是「auth 改动 / 迁移改动 / 发布前」，不是每次改代码。
# 判断类审计（威胁建模、复杂迁移评审、黑盒攻防思路）不在脚本里，
# 见 .agents/skills/security-review/SKILL.md 与 .agents/skills/sql-review/SKILL.md。
#
# 用法: ./scripts/audit.sh          （= make audit）
# 可调: AUDIT_SKIP_VULN=1 ./scripts/audit.sh   跳过 govulncheck（离线/省时）
set -uo pipefail

cd "$(dirname "$0")/.."

# 非登录 shell 通常没有 $(go env GOPATH)/bin，和 Makefile 保持一致地注入。
export PATH="$(go env GOPATH)/bin:$PATH"

failures=0
pass() { printf 'ok:   %s\n' "$1"; }
fail() { printf 'FAIL: %s\n' "$1" >&2; failures=$((failures + 1)); }

# 只在「将要入库」的文件里搜：已跟踪 + 未跟踪但未被 ignore 的。
# 新写的 .env 或粘贴进来的密钥在提交前就应被抓到，而 bin/、coverage.out 等构建产物已被 .gitignore 排除。
scoped_files() { git ls-files -z; git ls-files -oz --exclude-standard; }
tracked_grep() { scoped_files | xargs -0 grep -InE "$@" -- 2>/dev/null || true; }
# 去掉「注释行」与「已用 ${VAR} 占位的行」，避免 .env.example 的示例值误报。
strip_examples() { grep -vE '^[^:]+:[0-9]+:[[:space:]]*#' | grep -v '\${' || true; }

echo "==> 1/6 凭证与密钥泄露（将入库的文件）"
leaks=$(tracked_grep 'AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}')
if [ -n "$leaks" ]; then
	fail "疑似硬编码凭证/私钥:"
	printf '%s\n' "$leaks" >&2
else
	pass "无硬编码密钥、私钥、云厂商 token"
fi

# 只有 .env.example 允许入库；真实 .env 绝不进 git（AGENTS.md 硬性规则 11）。
envfiles=$(scoped_files | xargs -0 -n1 echo | grep -E '(^|/)\.env($|\.)' | grep -v '\.example$' || true)
if [ -n "$envfiles" ]; then
	fail "真实 .env 被 git 跟踪（必须 gitignore + 轮换其中密钥）: $(printf '%s' "$envfiles" | tr '\n' ' ')"
else
	pass "无真实 .env 入库"
fi

# 生产 JWT secret 必须来自环境注入：仓库里出现 ≥32 字节的字面量即为泄露。
jwt_literals=$(tracked_grep 'APP_AUTH_JWT_SECRET[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9+/=_-]{32,}' | strip_examples)
if [ -n "$jwt_literals" ]; then
	fail "APP_AUTH_JWT_SECRET 出现真实字面量（应由环境注入，≥32 字节随机值不进仓库）:"
	printf '%s\n' "$jwt_literals" >&2
else
	pass "APP_AUTH_JWT_SECRET 无硬编码字面量"
fi

echo "==> 2/6 日志卫生（内部包不得 print，zap 不得记录凭证）"
rawprint=$(grep -rnE '(^|[^a-zA-Z])(fmt\.Print|log\.Print)' internal/ --include='*.go' || true)
if [ -n "$rawprint" ]; then
	fail "internal/ 中出现非 zap 输出（违反「日志只用 zap」）:"
	printf '%s\n' "$rawprint" >&2
else
	pass "internal/ 无 fmt.Print / log.Print"
fi

logleaks=$(grep -rnE 'zap\.(String|ByteString|Any|Reflect)\("(password|passwd|new_password|old_password|token|access_token|refresh_token|secret|authorization|jwt|api_key)"' internal/ --include='*.go' || true)
if [ -n "$logleaks" ]; then
	fail "日志字段里出现凭证类键名（密码/token 禁止入日志）:"
	printf '%s\n' "$logleaks" >&2
else
	pass "日志无 password/token/secret 字段"
fi

echo "==> 3/6 可移植 SQL（测试库是 SQLite，禁止 PostgreSQL 专属语法）"
pgsql=$(grep -rnE '\bILIKE\b|\bJSONB\b|\bgen_random_uuid\b|\bSERIAL\b|[a-z_0-9)]::(text|int|int8|uuid|jsonb|numeric|bigint)' internal/ --include='*.go' || true)
if [ -n "$pgsql" ]; then
	fail "internal/ 出现 PostgreSQL 专属语法（SQLite 测试会炸，改用 LOWER(col) LIKE ? 等可移植写法）:"
	printf '%s\n' "$pgsql" >&2
else
	pass "无 PG 专属语法（ILIKE / jsonb / :: 强转 / SERIAL）"
fi

echo "==> 4/6 迁移文件成对且可回滚"
migration_bad=0
for up in migrations/*.up.sql; do
	down="${up%.up.sql}.down.sql"
	if [ ! -f "$down" ]; then
		fail "缺少 ${down}（up/down 必须成对）"
		migration_bad=1
	fi
done
for down in migrations/*.down.sql; do
	up="${down%.down.sql}.up.sql"
	if [ ! -f "$up" ]; then
		fail "缺少 ${up}（up/down 必须成对）"
		migration_bad=1
		continue
	fi
	# golang-migrate 对「只有注释/空白」的 down 会在回滚时报空语句错误。
	if ! grep -qE '^[[:space:]]*[A-Za-z]' "$down"; then
		fail "$down 没有任何 SQL 语句（回滚会失败）"
		migration_bad=1
	fi
done
[ "$migration_bad" -eq 0 ] && pass "迁移 up/down 成对且 down 非空"

echo "==> 5/6 鉴权默认拒绝（401 未认证 / 403 无权限 / 登录失败不泄露用户存在性）"
authz_tests='TestOpenAPISecurity|TestAnonymousRequestsAreRejected|TestMemberPermissions|TestLoginFailures|TestRefreshFlow|TestRefreshTokenHeaderTransportForAPIClients|TestLogoutIsImmediateEvenInsideReuseInterval|TestPasswordChangeInvalidatesTokens'
if go test ./internal/app -run "$authz_tests" -count=1; then
	pass "鉴权矩阵回归通过（401 / 403 / 登录失败 / refresh 轮换）"
else
	fail "鉴权矩阵回归失败（见上方 go test 输出）"
fi

echo "==> 6/6 依赖漏洞（govulncheck）"
if [ "${AUDIT_SKIP_VULN:-0}" = "1" ]; then
	echo "skip: govulncheck（AUDIT_SKIP_VULN=1）"
elif ! ./scripts/check-tools.sh govulncheck >/dev/null 2>&1; then
	fail "缺少 govulncheck（运行 make tools）"
elif govulncheck ./...; then
	pass "govulncheck 未发现已知漏洞"
else
	fail "govulncheck 发现漏洞（升级受影响的依赖）"
fi

echo
if [ "$failures" -eq 0 ]; then
	echo "audit: PASS（6/6 机械检查；威胁建模与复杂迁移评审仍需人工/skill 清单）"
	exit 0
fi
echo "audit: FAIL — $failures 项未通过" >&2
exit 1
