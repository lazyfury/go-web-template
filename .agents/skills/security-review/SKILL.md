---
name: security-review
description: go-web-template 的安全审计配方：先跑机械化门（make audit：密钥泄露/日志卫生/鉴权矩阵/依赖漏洞），再走人工判断清单（认证、授权与资源归属、注入、信息泄漏、限流），以及黑盒攻防的 curl 用例矩阵。用于"安全审计、上线前安全检查、auth 改动后的复核、渗透/攻防测试、越权验证"。
---

# 安全审计与黑盒攻防

**分工**：机械能判定的 → `make audit`（结果可信、可回归）；需要判断的 → 本文件清单；需要独立视角的 → 只读子 agent 跑同一份清单（边界见 `AGENTS.md`「子 agent 使用边界」）。

## 1. 机械门（先跑，别先读代码）

```bash
make audit        # 6 项：密钥泄露 / 日志卫生 / 可移植 SQL / 迁移配对 / 鉴权矩阵回归 / govulncheck
```

失败项的处理方向：

| 输出 | 含义 | 处理 |
|---|---|---|
| `疑似硬编码凭证` | 仓库里有 AKIA / 私钥 / gh token / `.env` | 撤销并轮换凭证，改成环境变量注入 |
| `APP_AUTH_JWT_SECRET 出现真实字面量` | 生产密钥进了仓库 | 轮换 + 环境注入（≥32 字节非占位值） |
| `internal/ 中出现非 zap 输出` | 绕过日志中间件的 print | 改用注入的 `*zap.Logger` |
| `日志字段里出现凭证类键名` | 密码/token 入日志 | 删字段，只留 `user_id` / `request_id` |
| `鉴权矩阵回归失败` | 401/403/登录失败行为变了 | 先修测试要的行为，再谈改契约 |
| `govulncheck 发现漏洞` | 依赖已知漏洞 | 升级依赖（`go mod tidy` 后重跑） |

只有 `make audit` 绿了再往下做判断类审计。

## 2. 判断清单（按攻击面，逐条给结论）

**认证 / 会话（`internal/auth`、`platform/authkit`）**

- 登录失败是否统一 401、不区分「用户不存在」与「密码错」？计时差异是否可忽略？
- 账号锁定/退避是否可被用来锁死他人（按 email 枚举 → DoS）？
- access token 校验是否同时检查 `token_version` 与 `exp`？改密/禁用后旧 token 是否立即失效？
- refresh token 是否只存 SHA-256、是否轮换、重放容忍是否只对 `revoked_reason=rotated` 且窗口内？会话是否被 `session_started_at` 封顶？
- cookie 是否 `HttpOnly` + `SameSite` + 生产 `Secure`？是否被写进响应体或 URL？

**授权（默认拒绝）**

- 每个 `/api/v1` 端点是否都在 `protected` 组？新增端点是否有 `authkit.RequirePermission`，`Op.Errors` 是否带 `CodeForbidden`？
- **资源归属**是否在 service 层用显式 `actor` 校验（不是只在中间件）？直接改 URL 里的 id 能否越权读写他人资源（IDOR）？
- 401 与 403 是否混用（无 token 却返回 403 = 泄露资源存在性）？
- 自我提权：普通用户能否 PATCH 自己的 `role`/`status`？

**输入 / 输出**

- 请求体上限是否生效（超大 body 是否 413 而非 OOM）？`binding` tag 是否覆盖所有字段？
- 错误响应是否只含 `{code,message,data}`，不泄露 SQL/栈/内部路径？
- 响应 DTO 是否与 GORM entity 分离（`password_hash`、`token_version`、`deleted_at` 不得出现）？
- 列表接口的排序/过滤字段是否白名单（防注入式 `ORDER BY` 拼接）？

**运行面**

- 限流是否只按 IP（被 NAT 后互相影响）？是否覆盖登录端点？
- CORS/安全头是否只在需要时放开？`/metrics` 是否暴露给公网？
- 健康检查是否泄露配置/依赖版本？

## 3. 黑盒攻防矩阵（需 DB；`make db-up && make migrate-up`）

起服务自测用 `make smoke`（自起自停 + 401 断言）。手工打点用下面的矩阵，`BASE` 指向本机端口：

```bash
BASE=http://127.0.0.1:18080        # curl 必须 --noproxy '*'（本机 all_proxy 会挂住）
c() { curl -s --noproxy '*' -o /dev/null -w '%{http_code}\n' "$@"; }
```

| # | 攻击 | 期望 |
|---|---|---|
| 1 | `c $BASE/api/v1/users` 匿名 | 401（不是 403/200） |
| 2 | 无 token 带伪造 `Authorization: Bearer x.y.z` | 401 |
| 3 | member token 打管理员端点（列表/删除） | 403 |
| 4 | member 用自己 token 读/改/删他人 id | 403 或 404（不得 200） |
| 5 | member PATCH 自己的 `{"role":"admin"}` | 403（不得变管理员） |
| 6 | 登录：错密码 / 不存在邮箱 / 空 body | 401 / 401 / 400，message 不得区分前两者 |
| 7 | 连续错密码 N 次 | 锁定或退避生效，响应仍是 401 |
| 8 | 登出后用旧 access token | 401（`token_version` 已提升） |
| 9 | 登出后用旧 refresh cookie | 401，且并发的其他会话是否被误杀需如实记录 |
| 10 | 超大 body（`head -c 20M /dev/zero`） | 413，服务不崩 |
| 11 | 列表 `page_size=100000` / `page=-1` | 400 或夹到上限，不得全表返回 |
| 12 | 路径穿越/编码畸形 `users/%2e%2e%2f` | 404，不落 panic（看日志有无 stack） |

逐条记录真实状态码；`docs/troubleshooting.md` 里已有的坑（代理挂起、端口被孤儿进程占用）先看再动手。

## 4. 输出格式（审计结论必须可执行）

每条发现写：

```
[严重度 高/中/低] 标题
位置: internal/auth/service.go:151
复现: curl -s --noproxy '*' -o /dev/null -w '%{http_code}' -X POST ...   → 200（期望 403）
影响: 一句话说清谁能拿到什么
建议: 具体到函数/中间件，不要"加强校验"
```

**边界**：审计阶段**只读**——不改代码、不 `make migrate-down`、不对共享环境做写操作。要验证的破坏性动作先问人（见 `AGENTS.md`）。无发现时明确写"已覆盖第 1/2/3 节的检查项，未发现 X 类问题"，不要只写"没问题"。
