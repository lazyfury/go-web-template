#!/usr/bin/env bash
# 检查外部工具是否可用；缺失时给出可执行的修复指令并退出 1。
# 用法: ./scripts/check-tools.sh golangci-lint air govulncheck migrate
set -uo pipefail

missing=()
for cmd in "$@"; do
  command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
done

if [ ${#missing[@]} -gt 0 ]; then
  echo "缺少工具: ${missing[*]}" >&2
  echo "修复: make tools（版本 pin 在 Makefile 顶部）" >&2
  echo "安装位置通常是 \$GOPATH/bin，确认它已加入 PATH" >&2
  exit 1
fi
