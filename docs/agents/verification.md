# 验证与质量门

> 入口见 `../../AGENTS.md`；命令以 `../../Makefile` / `make help` 为准。

## 三层门（不要混用）

| 门 | 何时跑 | 覆盖什么 |
|---|---|---|
| `make check` | **每次提交前（必跑）** | golangci-lint、gofmt 校验、go vet、race 测试、覆盖率门（`scripts/cover-check.sh`：总量 `COVER_MIN` + 每包 `COVER_PKG_MIN` + 关键文件零覆盖函数）、build、OpenAPI 黄金文件、`go mod tidy` 同步 |
| `make audit` | auth/JWT/鉴权/迁移 SQL 改动、发布前 | 密钥泄露、日志卫生、可移植 SQL、迁移 up/down 配对、鉴权 401/403 矩阵回归、govulncheck |
| `make migrate-roundtrip` | 改迁移 SQL、发布前 | 真实 PG 的 scratch 库上跑 `up → down -all → up`（先 `make db-up`） |
| `make e2e` | 改前端路由/登录态/关键页面、发布前 | 真实浏览器 + 后端 + PG：登录 → 建用户 → 删用户 → 退出（需 `make dev` + seed 管理员） |
| `make smoke` / `make verify-auth` | 有 postgres 时；改认证/会话后必跑 `verify-auth` | 真实进程 + 真实 PG 的端到端行为；`smoke` 覆盖探针/登录/权限/CRUD/refresh/OpenAPI |

## 常用闭环

```bash
make test          # 纯 Go 改动最快闭环，内存 SQLite，不需要 docker
make check         # 提交前唯一门槛（Go）
make ci            # 全量门 = check + web-check + audit（本地提交前 / 接入 CI）
make audit         # auth/鉴权/迁移/发布前
make smoke         # 有 PG 时；脚本自起自停
make verify-auth   # 改登录/refresh/会话后必跑
make migrate-roundtrip  # 改迁移 SQL 后必跑（真实 PG，scratch 库，不碰开发库）
make bench          # 改 JWT/限流等热路径后对比 ns/op 与 allocs
make fuzz FUZZTIME=30s  # 改 token 解析/输入校验后跑一轮
```

- 前端改动：`make web-check`（eslint + vue-tsc + vitest 覆盖率门槛 + `pnpm api:check` + build）；首次在新机器上先 `make web-install`。
- 不想记组合就跑 `make ci`；它**不包含**需要真实 PG 的 `make smoke` / `make verify-auth`。`pnpm api:check` 会重新生成 `src/types/openapi.ts` 并用 `git diff --exit-code` 判断漂移。
- 改了路由/`routekit.Op`：`make openapi` 并提交 `docs/openapi.json`。
- 工具缺失：`make tools`；版本 pin 在 Makefile 顶部，不要 `@latest`。

## 证据要求

- **禁止未运行就声称通过**：最终回复贴真实输出的最后几行。
- `make check` 至少贴 `0 issues.`、`cover-check: ok`（含每包明细）与 `tidy-check: ok`。
- auth/鉴权/迁移：附 `make audit` 的 `audit: PASS` 与 govulncheck 结果。
- 有 PG：附 `make smoke` / `make verify-auth` 结果。
- 没有环境跑的门必须写进“未验证项”，不要模糊成“应该没问题”。
- 前端改动：贴 `pnpm typecheck` / `pnpm build` / `pnpm api:check` 结果。

## 什么时候只跑子集

| 改动 | 最小验证 |
|---|---|
| 只改文档 | `git diff --check`；不需要 `make check` |
| 纯 Go 业务逻辑 | `make test`，提交前仍跑 `make check` |
| 改路由/DTO/OpenAPI | `make openapi` + `make check` + `pnpm api:check` |
| 改 auth/会话 | `make check` + `make audit` + `make verify-auth` |
| 改迁移 SQL | `make check` + `make audit` + `make migrate-roundtrip`（真实 PG） |
| 改前端 | `pnpm typecheck` + `pnpm build` + `pnpm api:check` |

## 常见失败

- OpenAPI 黄金文件漂移：`make openapi`，review diff 后提交。
- 前端类型漂移：`make openapi` → `pnpm gen:api`。
- `tidy-check` 失败：`go mod tidy` 后提交 `go.mod/go.sum`。
- lint 报错：见 `docs/troubleshooting.md` 的 golangci-lint 表。
