# 安全演练记录（2026-10，黑盒 + 修复）

> 一次针对**真实进程 + 真实 PostgreSQL** 的攻防演练：先按其结果改代码，再用同一套
> 攻击复测。所有结论都带可复制命令与真实状态码，不写"应该没问题"。
> 配方见 `skills/security-review/SKILL.md`；机械门见 `scripts/audit.sh`（`make audit`）。

## 1. 范围与方法

| 项 | 内容 |
|---|---|
| 目标 | 本机 compose 编排（`APP_ENV=production`，`http://127.0.0.1:8080` + PG 16） |
| 方法 | 黑盒：无认证侦察 → 认证/会话 → 授权/IDOR → 输入/注入 → 资源耗尽 → 运行面 |
| 形态 | 只读优先；写操作仅限自建测试账号与本地库；不碰数据卷、不跑 `migrate-down` |
| 时机 | 每轮改动后重跑同一矩阵（见 §6），修复必须由真实状态码背书 |

**为什么机械门不够**：`make audit` 在演练前是 6/6 PASS、govulncheck 0 漏洞，但下面
F1/F2/F3 三条都真实可利用。`audit` 只认静态模式，黑盒行为（默认密钥是否可签、
限流键是否可伪造、失败响应是否可区分）只能靠打。

## 2. 结论摘要

| 编号 | 严重度 | 问题 | 状态 |
|---|---|---|---|
| F1 | 严重 | 生产编排用仓库自带默认 JWT 密钥 → 可自签 admin token | 已修 `d09f857` |
| F2 | 高 | 限流键取自客户端 `X-Forwarded-For` → 绕过 + bcrypt CPU DoS | 已修 `a0c4ddd` |
| F3 | 中 | 锁定/禁用返回 403 + 剩余时间 → 邮箱枚举 + 定向锁定 DoS | 已修 `0623e26` |
| F4 | 中 | 登出只吊销当前 refresh，未清全账号（与 design-auth 不符） | 已修 `4e0d1fe` + `6af2cfc` |
| F5 | 中 | 生产式编排里 refresh cookie 无 `Secure` | 已修 `3ecde19` |
| F6 | 中低 | 无安全响应头；含 token 的响应可被缓存 | 已修 `012a821` |
| F7 | 中低 | `/metrics` 公网可达（Go 版本 + 路由/流量画像） | 已修 `55468a6` |
| F8 | 低 | 改密端点无按用户限流（bcrypt 可被滥用） | 已修 `cca5676` |
| F9 | 低 | 成员 PATCH 自己 email 的 409 构成邮箱枚举 | 已修 `1ee29a0` |
| F10 | 低 | JWT `sub` 宽松解析（`"1 or 1=1"` → uid 1） | 已修 `2f980ec` |
| F11 | 低 | 客户端可控制 `X-Request-Id`（日志/追踪伪造） | 已修 `62c173b` |
| F12 | 低 | 列表响应回显未归一化的 `page`/`page_size` | 已修 `559a8a0` |

## 3. 关键发现详情（复现式）

### F1 仓库自带 JWT 密钥 = 无认证（严重）

```bash
# 容器实际密钥（compose 旧默认值，36 字节，过了长度校验）
docker inspect go-web-template-app-1 --format '{{range .Config.Env}}{{println .}}{{end}}' | grep JWT_SECRET
# 用它签 {"iss":"go-web-template","sub":"1","exp":now+900,"role":"admin","ver":1,"typ":"access"}
curl -s --noproxy '*' -H "Authorization: Bearer $FORGED" http://127.0.0.1:8080/api/v1/auth/me
# 修复前：200 {"id":1,"email":"admin@example.com","role":"admin"}；修复后：401
```

根因：`isPlaceholderSecret` 只认 `change_me`/`insecure`/`changeme`，
`local-compose-default-jwt-secret-32b` 一条不命中 → production 照常启动。
修复：标记集合扩到 `local-compose-default`/`dev-only`/`example`/`replace-with`/`your-secret`…；
compose 不再提供可用默认值（留空即 fail-fast）；`make docker-up` 生成 `.local/jwt-secret`；
`TestRepoConfigSecretsAreRejected` 扫描仓库配置文件（含 `${VAR:-default}` 形态）作为回归钉子。

### F2 `X-Forwarded-For` 绕过限流 + CPU DoS（高）

```bash
# 40 并发，每请求换一个 XFF
seq 1 40 | xargs -P 40 -I{} curl -s --noproxy '*' -o /dev/null -w '%{http_code}\n' \
  -X POST -H 'Content-Type: application/json' -H "X-Forwarded-For: 10.9.0.{}" \
  -d '{"email":"nobody@example.com","password":"wrongpass123"}' \
  http://127.0.0.1:8080/api/v1/auth/login | sort | uniq -c
# 修复前：40×401（0 个 429）；同 IP 是 10×401 + 30×429
# 修复后：10×401 + 30×429（与不伪造一致）
```

200 并发唯一 XFF：修复前 wall 5.31s、`/healthz` p50 **297ms**（bcrypt 打满 10 核）；
修复后 190×429、wall 0.32s、p50 **6ms**。访问日志里的 `ip` 也从可伪造变成真实 TCP 对端。

修复：`http.trusted_proxies`（默认空 = 不信任任何代理）→ `SetTrustedProxies`，
限流/日志统一用 `c.ClientIP()`。

### F3 锁定提示 = 邮箱枚举 + 定向锁定（中）

```bash
for i in $(seq 1 6); do curl -s --noproxy '*' -w ' [%{http_code}]\n' -X POST \
  -H 'Content-Type: application/json' -H "X-Forwarded-For: 10.6.0.$i" \
  -d '{"email":"smoke-admin@example.com","password":"wrongpass123"}' \
  http://127.0.0.1:8080/api/v1/auth/login; done
# 修复前：第 5 次起 403 "account temporarily locked, retry in 15m0s"（不存在的邮箱永远 401）
# 修复后：6 次都是 401 invalid credentials；锁定仍发生在服务端（日志 reason=locked）
```

修复：锁定/禁用与"密码错"同形（401 + 同样 message）+ 补 `BurnTiming`
（否则锁定账号会快 ~200ms，时序照样泄露：实测修复后 0.1997s vs 0.1994s）。

### F4 登出不彻底（中）

```bash
# 两个会话；会话 2 先轮换一次 cookie，再登出会话 1
# 修复前：会话 2 当前 cookie → refresh 200；被轮换的旧 cookie（30s 内）→ refresh 200
# 修复后：两者都 401
```

修复：`Logout` → `revokeAllSessions`（吊销全部活跃 token）；并且把该用户
`revoked_reason=rotated` 的历史行改标（`RetireRotatedForUser`），否则宽限期会把
刚轮换掉的 token 复活。同一助手用于改密/管理员重置/重用检测/会话上限。

### F5–F12（部署与契约类）

- **F5**：compose 默认 `APP_AUTH_COOKIE_SECURE=false` 与 `configs/config.yaml` 注释
  （"镜像默认必须 true"）自相矛盾 → 默认改 `true`，本地由 `make docker-up` 显式注入 `false`。
- **F6**：响应只有 `X-Request-Id` → 加 `SecurityHeaders`（nosniff / XFO / Referrer-Policy，
  HSTS 仅 production）+ `/api/v1` 的 `Cache-Control: no-store`。
- **F7**：`/metrics` 匿名 200（575 行，含 `go_info{version}` 与全路由标签）→
  `metrics.enabled`（`APP_METRICS_ENABLED`），生产式编排默认关闭。
- **F8**：30 并发错当前密码 → 30×401、0×429 → 按用户分桶（`auth.password_rps/burst`），
  实测 5×401 后 429，其他端点不受影响。
- **F9**：成员 PATCH 自己 email 命中 409 / 未命中 200 → 成员禁止改 email（管理员专属），
  已注册与未注册都只返回 403。
- **F10**：`fmt.Sscanf("%d")` 接受 `"1 or 1=1"` → `strconv.ParseUint` + 规范形式校验。
- **F11**：`X-Request-Id: forged id with spaces` 被原样回显 → 只接受 `[A-Za-z0-9._:-]{1,64}`，
  其余生成 uuid。
- **F12**：`page_size=100000` 回显 100000（实际 LIMIT 20）→ 回显归一化后的值。

## 4. 已确认有效（攻击后仍成立，不是"看起来没问题"）

| 攻击 | 结果 |
|---|---|
| 匿名打 `/api/v1/*`、路径/方法绕过（`//`、`./`、`%2e%2e`、大小写、`OPTIONS`/`TRACE`、尾斜杠 301） | 401 / 404，301 目标仍 401 |
| `alg=none`、`HS512`、错 secret、错 issuer、过期、`ver` 不匹配、`typ=refresh`、`sub=0` | 全 401（算法钉死有效） |
| member 打 admin 端点、自提权 `role`/`status`、批量赋值 `token_version`/`deleted_at` | 403（拒绝而非静默忽略） |
| 他人资源 IDOR（`/users/:id`、`/messages/inbox/:id`、`:id/read`） | 403 / 404；不存在的 id 同样 403（无存在性泄露） |
| SQL 注入（`keyword` 多种 payload、字典 group 路径） | 全参数化；无 ORDER BY 拼接面 |
| 请求体上限 | 20MB→413、1.2MB→413、chunked 无 CL→400、5000 层嵌套→400、CT 混淆→400 |
| 登录时序（未知邮箱 vs 密码错 vs 锁定） | 均 ~200ms（`BurnTiming`） |
| 改密/禁用后旧 access+refresh、超窗重放 | 立即 401，全会话撤销 |
| 日志卫生 | 无密码/token 落库；错误响应无 SQL/栈；metrics 标签用路由模板（无 PII） |

## 5. 残留风险与未覆盖

- **锁定 DoS（接受）**：5 个匿名请求可让已知账号锁 15 分钟（不可续期、响应不可观测）。
  要彻底消除需改成按 IP+账号退避而非硬锁，属于策略变更。
- **access token 无状态**：登出/改密后旧 access token 最多再用到自然过期（≤ `auth.access_ttl`）。
  需要"立刻失效"就走改密/禁用（`token_version++`）。
- **多副本吊销窗口（接受）**：`auth.verify_cache_ttl`（默认 5s）让另一台副本最多滞后一个 TTL 才看到
  改密/禁用/删除；本进程内一律即时失效（`auth.Service.InvalidateAuthState` + `user.Service` 的写路径）。
  宁可把窗口写在这里，也不假装它是 0；要严格即时就把 TTL 置 0（换取每请求一读）。
- **SSRF（已修，2026-10 复测）**：`WebhookSender` 的私网拦截从「只检查配置里的 URL」改成**拨号时校验真实对端地址**
  （`net.Dialer.Control`），因此 302 跳转与 DNS rebinding 都打不到私网；同时禁用出站代理（否则真实目标藏在代理后面）
  并把重定向收敛到 5 跳。本次仍**未做**真实公网端点的黑盒验证（无可用外网 webhook 目标），只有单测背书：
  `internal/message/sender_test.go` 的 `TestWebhookDialControlRejectsNonPublicAddresses` /
  `TestWebhookClientBlocksRedirectToPrivateTarget` / `TestWebhookClientCapsRedirects`。
- **slowloris（已修，2026-10 复测）**：`http.Server` 现在显式设置 `ReadHeaderTimeout`
  （`http.read_header_timeout`，默认 5s，不得超过 `read_timeout`），header 阶段不再能霸占整条 read deadline。
  回归钉在 `internal/web/server_test.go`（字段真接线）与 `loader_test.go`（默认值与上限）；
  stdlib 自己负责剩余行为，本次未做真实慢连接压测。
- **反代多级 XFF（已做端到端）**：`make verify-auth` 的 H 段跑真实进程：默认「不信任任何代理」时
  轮换 `X-Forwarded-For` 骗不到新限流桶（仍 429）；把 `127.0.0.1` 配成可信代理后，每个 XFF 才独立成桶，
  且同一 XFF 打满突发量仍 429。单一反代的真实压测仍未做（只有这两条断言）。
- **未覆盖**：会话绝对上限的长周期行为（仅 `verify-auth` 的 3s 压缩验证）。

## 6. 复测手册

```bash
make check                      # lint/gofmt/vet/race/build/OpenAPI 黄金文件/tidy
make audit                      # 6 项机械门（含鉴权矩阵回归、govulncheck）
make verify-auth                # 真实进程 + 真实 PG：宽限期/登出立即/上限夹紧/密钥轮换/校验缓存/XFF
make docker-up                  # 起编排（自动生成 .local/jwt-secret、关 metrics/swagger）

BASE=http://127.0.0.1:8080      # curl 必须 --noproxy '*'（本机 all_proxy 会挂住）
curl -s --noproxy '*' -o /dev/null -w '%{http_code}\n' $BASE/api/v1/users          # 401
curl -s --noproxy '*' -o /dev/null -w '%{http_code}\n' $BASE/metrics               # 404（默认关）
curl -s --noproxy '*' -D- -o /dev/null $BASE/api/v1/auth/me | grep -i cache-control # no-store
```

新增/修改安全相关代码时，请把对应断言补进 `internal/app/integration_test.go`
（401/403/同形失败/限流）或 `scripts/verify-auth.sh`（真实进程行为），不要只靠本次记录。
