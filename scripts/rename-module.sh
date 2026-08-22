#!/usr/bin/env bash
# 把模板的模块路径替换成你自己的，例如：
#   ./scripts/rename-module.sh github.com/yourorg/your-project
# 会更新 go.mod、所有 import 以及 golangci 的 local-prefixes。
set -euo pipefail

OLD="github.com/example/go-web-template"
NEW="${1:?用法: ./scripts/rename-module.sh github.com/yourorg/your-project}"

if [[ "$NEW" == "$OLD" ]]; then
  echo "新旧模块路径相同，无需修改"
  exit 0
fi

echo "==> 替换模块路径: $OLD -> $NEW"
grep -rl "$OLD" --include="*.go" --include="go.mod" --include=".golangci.yml" . \
  | xargs sed -i '' "s|$OLD|$NEW|g"

echo "==> go mod tidy"
go mod tidy

echo "==> 完成。验证: grep -r '$NEW' --include='*.go' . | head"
