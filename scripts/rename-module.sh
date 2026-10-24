#!/usr/bin/env bash
# 把模板的模块路径替换成你自己的，例如：
#   ./scripts/rename-module.sh github.com/yourorg/your-project
# 会更新 go.mod、所有 import、golangci 的 local-prefixes，以及文档中的模块路径引用。
set -euo pipefail

cd "$(dirname "$0")/.."

OLD="github.com/example/go-web-template"
NEW="${1:?用法: ./scripts/rename-module.sh github.com/yourorg/your-project}"

if [[ "$NEW" == "$OLD" ]]; then
  echo "新旧模块路径相同，无需修改"
  exit 0
fi

echo "==> 替换模块路径: $OLD -> $NEW"

# 同时覆盖文档（README/context 等），否则重命名后文档里的模块路径会立刻过期。
# 用 perl 而非 `sed -i ''`：后者只有 BSD/macOS 支持，GNU sed 会报错。
files=$(grep -rl \
  --exclude-dir=.git \
  --exclude-dir=node_modules \
  --exclude-dir=dist \
  --include="*.go" --include="go.mod" --include=".golangci.yml" --include="*.md" \
  "$OLD" . || true)

if [[ -z "$files" ]]; then
  echo "未找到包含 $OLD 的文件，检查是否已替换过"
  exit 1
fi

echo "$files" | while IFS= read -r f; do echo "    $f"; done
echo "$files" | xargs perl -pi -e "s{\Q$OLD\E}{$NEW}g"

echo "==> go mod tidy"
go mod tidy

echo "==> 完成。验证:"
echo "    grep -rn '$OLD' --include='*.go' --include='*.md' . | grep -v node_modules   # 应无输出"
echo "    make check"
