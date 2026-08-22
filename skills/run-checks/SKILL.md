---
name: run-checks
description: 对本仓库执行提交前全量质量检查：gofmt → go vet → golangci-lint → go test（含 race）→ 覆盖率。给出标准命令、通过标准与常见失败的修复方法。
whenToUse: 用户要求"检查/验证/跑一遍质量检查"，或提交代码前需要确认 build/vet/lint/test 全绿时。
---

# 质量检查流程

按顺序执行；任一环节失败先修复再继续，不要跳过。

## 1. 格式

```bash
gofmt -l .            # 输出为空 = 通过；有输出则 gofmt -w <file>
make fmt              # 需要 goimports（已安装）；goimports 会按 .golangci.yml 的 local-prefixes 分组 import
```

## 2. 编译与静态检查

```bash
make build            # go build ./...（也验证 bin/ 产物）
make vet              # go vet ./...
make lint             # golangci-lint run（配置见 .golangci.yml）
```

lint 通过标准：零输出。常见修复：

| 报错 | 修复 |
|---|---|
| `exported: ... should have comment` | 导出标识符补注释（含 const 块内每一项） |
| `hugeParam: cfg is heavy` | 大结构体参数改传指针 |
| `unnecessaryBlock` | 去掉多余的 `{ }` 块 |
| `unnamedResult` | 返回值命名 |
| `prealloc` | `var x []T` → `x := make([]T, 0, n)` |
| `ifElseChain` | 改 switch |
| `ineffectual assignment` | 未用变量改 `_` |

## 3. 测试

```bash
make test             # go test ./... -count=1
make test-race        # go test -race ./... -count=1（提交前建议跑）
make coverage         # 生成 coverage.out 并打开 HTML
```

- 测试不依赖 Docker/PostgreSQL（内存 SQLite）。
- 失败排查：`ILike` 语法错误 → 查询写了 PostgreSQL 专属语法；`import cycle` → handler 测试 import 了 router（集成测试应放 router 包）；`expected conflict error` → 确认 `apperrors.Error.Is` 按 Code 匹配、mock 返回了正确的错误码。

## 4. 收尾

```bash
git status --short    # 确认无意外文件（如 .env、coverage.out 不应入库）
```

全部通过后再提交。若改动涉及 Swagger 注解/迁移/配置，另行执行 `make swagger` / `make migrate-*`（见对应 skill），并把生成物一并纳入提交。
