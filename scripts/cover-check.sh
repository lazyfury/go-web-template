#!/usr/bin/env bash
# 覆盖率门（make check 的一部分）：
#   1. 总量下限 COVER_MIN；
#   2. 每个包的覆盖率下限 COVER_PKG_MIN（防止某个包整体腐烂，而总量被集成测试拉高掩盖）；
#   3. 关键文件里任何「有语句但 0% 覆盖」的函数直接失败（防止新函数没人测）。
#
# 用法: ./scripts/cover-check.sh            （先由 make cover-check 生成 coverage.out）
# 可调: COVER_MIN=78 COVER_PKG_MIN=60 ./scripts/cover-check.sh
set -uo pipefail

cd "$(dirname "$0")/.."

COVER_MIN=${COVER_MIN:-76}
COVER_PKG_MIN=${COVER_PKG_MIN:-55}
PROFILE=${COVER_PROFILE:-coverage.out}

# 显式豁免：二进制胶水（cobra 命令、配置装载、DB 打开、信号处理）没有独立单测，
# 只能靠真实进程验证。它们仍计入总量，只是不参与包级下限。
EXEMPT_PKGS="internal/cli"

# 关键文件：新增 auth/限流/发送路径时把文件加进来。规则是「不能有 0% 的函数」，
# 不要求高覆盖率——先保证每条关键路径至少被执行过一次。
CRITICAL_FILES="
internal/auth/service.go
internal/platform/authkit/token.go
internal/platform/authkit/rbac.go
internal/platform/authkit/middleware.go
internal/platform/passwd/passwd.go
internal/platform/redisx/ratelimit.go
internal/web/middleware_ratelimit.go
internal/user/repository.go
"

if [ ! -f "$PROFILE" ]; then
	echo "FAIL: $PROFILE 不存在（先运行 make cover-check）" >&2
	exit 1
fi

failures=0

total=$(go tool cover -func="$PROFILE" | tail -1 | awk '{ gsub(/%/, "", $3); print $3 }')
printf 'coverage total: %.1f%% (floor %s%%)\n' "$total" "$COVER_MIN"
if awk -v t="$total" -v m="$COVER_MIN" 'BEGIN { exit !(t + 0 < m + 0) }'; then
	echo "FAIL: 总覆盖率低于下限" >&2
	failures=$((failures + 1))
fi

# 按包聚合：coverprofile 里同一个 block 会按 -coverpkg 里的每个测试包各出现一次，
# 必须先按 block 去重（取最大命中数），否则分母被重复累计。
if ! awk -v min="$COVER_PKG_MIN" -v exempt="$EXEMPT_PKGS" '
function pkg_of(file,   parts, n) {
	sub(/^github\.com\/example\/go-web-template\//, "", file)
	n = split(file, parts, "/")
	if (parts[1] == "internal" && parts[2] == "platform" && n >= 3) return parts[1] "/" parts[2] "/" parts[3]
	if (n >= 2) return parts[1] "/" parts[2]
	return parts[1]
}
FNR > 1 {
	loc = $1; st = $2 + 0; c = $3 + 0
	if (!(loc in stmts)) stmts[loc] = st
	if (c > seen[loc]) seen[loc] = c
}
END {
	for (loc in stmts) {
		file = loc; sub(/:.*/, "", file)
		p = pkg_of(file)
		tot[p] += stmts[loc]
		if (seen[loc] > 0) cov[p] += stmts[loc]
	}
	split(exempt, ex, " ")
	bad = 0
	for (p in tot) {
		pct = 100 * cov[p] / tot[p]
		isex = 0
		for (i in ex) if (p == ex[i]) isex = 1
		if (isex) {
			printf "skip %-42s %5.1f%% (%d stmts, exempt)\n", p, pct, tot[p]
		} else if (pct + 0 < min + 0) {
			printf "FAIL %-42s %5.1f%% < %s%%\n", p, pct, min
			bad = 1
		} else {
			printf "ok   %-42s %5.1f%%\n", p, pct
		}
	}
	exit bad
}' "$PROFILE" | sort -k2; then
	echo "FAIL: 存在低于每包下限的包" >&2
	failures=$((failures + 1))
fi

# 关键文件里不允许出现「有语句但 0%」的函数。
crit_one_line=$(printf '%s' "$CRITICAL_FILES" | tr '\n' ' ')
zero=$(go tool cover -func="$PROFILE" | awk -v crit="$crit_one_line" '
BEGIN { n = split(crit, files, " "); for (i = 1; i <= n; i++) if (files[i] != "") c[files[i]] = 1 }
$3 == "0.0%" {
	path = $1; sub(/:[0-9]+:$/, "", path)
	sub(/^github\.com\/example\/go-web-template\//, "", path)
	for (f in c) if (index(path, f) == 1) { printf "%s %s\n", $1, $2 }
}')
if [ -n "$zero" ]; then
	echo "FAIL: 关键文件存在 0% 覆盖的函数（至少给一条执行路径）:" >&2
	printf '%s\n' "$zero" >&2
	failures=$((failures + 1))
else
	echo "ok   关键文件无 0% 函数"
fi

if [ "$failures" -eq 0 ]; then
	echo "cover-check: ok"
	exit 0
fi
echo "cover-check: FAIL — $failures 项未通过" >&2
exit 1
