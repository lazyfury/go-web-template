# 运维 Runbook（备份 / 轮换 / 故障处置 / 发布）

> 面向值班与发布：先照命令执行，再回 `docs/security-review-2026-10.md` / `context.md` 查设计原因。
> 所有命令以 `Makefile` 为准；这里的“已验证”只写本机真实跑过的结果。

## 1. 备份与恢复

### 1.1 备份

```bash
make db-backup          # → backups/go_web_template-<YYYYmmdd-HHMMSS>.sql.gz
```

实现：compose 的 postgres 容器内 `pg_dump --no-owner --no-privileges` → 宿主机 `gzip`。
`backups/` 已 gitignore。放到对象存储/异地由调度器负责，例如：

```cron
17 3 * * * cd /srv/go-web-template && make db-backup && \
  aws s3 cp backups/ s3://my-backups/go-web-template/ --recursive --exclude "*" --include "*.sql.gz"
```

### 1.2 恢复（破坏性）

**先问人**：共享/预发/生产库恢复属于 `docs/agents/destructive-operations.md` 里必须先确认的操作。

1. 停写：把 app 摘流（`docker compose stop app` 或反代 503），否则恢复期间的新写入会丢；
2. 备份现状（恢复也可能失败，失败时不能连退路都没有）：`make db-backup`；
3. 执行恢复（`CONFIRM=1` 是硬门槛，缺了会直接拒绝）：

   ```bash
   FILE=backups/go_web_template-20261028-194703.sql.gz CONFIRM=1 make db-restore
   ```

4. 验证：`curl --noproxy '*' -o /dev/null -w '%{http_code}\n' localhost:8080/readyz` 应为 200，
   再抽查关键表行数与登录；
5. 放流量。

**本次演练（已验证）**：备份 → 恢复到独立 scratch 库 → `users` 行数与
`schema_migrations` 版本（9）与源库一致 → 删除 scratch 库。

### 1.3 已知边界

- 只有**逻辑备份**，没有 WAL 归档/PITR：RPO = 备份间隔，RTO ≈ dump 恢复时长。
  需要分钟级 RPO 时上 `pgBackRest`/云厂商 PITR，模板不内置。
- `pg_dump` 单事务导出，大库恢复慢；生产建议 `-Fc` + `pg_restore --jobs=4`（本仓库脚本保持
  SQL.gz 是为了“一条命令可读可查”）。
- 恢复出来的库**不含** `.local/jwt-secret`：旧 access token 仍由密钥环校验，别同时轮换密钥。

## 2. JWT 密钥轮换演练

无状态 access token + `kid` 密钥环（`authkit.TokenIssuer`），轮换不掉线：

1. 生成新密钥（≥32 字节）：`openssl rand -base64 48`；
2. 设置 `APP_AUTH_JWT_SECRET=<新>`、`APP_AUTH_JWT_KEY_ID=k2`，
   旧密钥放 `APP_AUTH_JWT_RETIRED_SECRETS=<旧>`（只验不签）；
3. 滚动重启；期间新旧 token 都可用（`make verify-auth` 的 F 段就是这个断言）；
4. 等 `auth.access_ttl`（默认 15m）过去，清空 `RETIRED_SECRETS` 完成退役。

回滚：把新密钥从 `JWT_SECRET` 撤下、旧密钥放回 `JWT_SECRET` 并保留 `k2` 在退休列表里即可
（退休列表同样能验签，但生产要求 ≥32 字节且非占位值）。

## 3. 数据库口令轮换

1. `ALTER USER postgres WITH PASSWORD '<新>';`（在 postgres 容器内 `psql`）；
2. 更新密钥管理里的 `APP_DATABASE_PASSWORD`，滚动重启 app；
3. 旧连接由 `database.conn_max_lifetime`（默认 1h）自然淘汰，期间两套口令并存不影响；
4. 验证 `/readyz` 与一条写请求；确认无 `password authentication failed` 日志后再清理旧凭据。

## 4. 故障处置 Playbook

| 症状 / 告警 | 第一检查 | 处置 |
|---|---|---|
| `AppDown`（`up{job=...}==0`） | 容器状态、启动日志首 20 行 | 配置/迁移失败会 fail-fast：看 `load config`、`database schema`、`init application` |
| `/readyz` 503 `database unreachable` | `pg_isready`、连接数 | 恢复 DB；连接池满看 `db_pool_in_use_connections` vs `db_pool_max_open_connections` |
| `/readyz` 503 `schema behind` / `dirty` | `SELECT * FROM schema_migrations` | 漏跑迁移 → `make migrate-up`；`dirty=true` 需人工修复（先备份），不要盲目 down |
| `High5xxRate` / `HighLatencyP95` | 访问日志按 `request_id`、`db_pool_wait_duration_seconds_total` | DB 慢查（`slow sql` 日志）→ 加索引/限流；池等待高 → 调 `database.max_open_conns` 或查泄漏 |
| `MessageWorkerStalled` | `worker_last_success_timestamp_seconds{worker="message_delivery"}`、`message_delivery_attempts_total` | 卡在 SMTP/Webhook 超时或 DB 锁；worker 出错会退出进程（同时 AppDown） |
| `DeliveryDead` / `DeliveryFailureRatio` | 投递记录页 / `message_deliveries.last_error` | 修通道（凭证/白名单）后管理员重试；持续失败先停 worker 防雪崩 |
| `HousekeepingStalled` / `HousekeepingTaskFailing` | 任务日志、磁盘占用 | 大 DELETE 锁等待 → 缩短窗口/加索引；磁盘满时先扩盘再跑清理 |
| `RateLimitStoreErrors` | Redis 连通性 | 限流 fail-open，认证仍安全（锁定在 DB）；恢复 Redis 或切回 `memory`（单副本） |
| `RefreshTokenReuseDetected` | `auth_refresh_total{result="reuse"}` 与用户是否多设备 | 真实盗用会全量吊销；确认是浏览器竞态则调 `auth.refresh_reuse_interval` |
| 磁盘持续增长 | `housekeeping_purged_rows_total`、表大小 | `retention.enabled` 是否关闭、窗口是否过大；先扩盘再处理 |
| 误删数据 | 备份时间点 | 按 §1.2 恢复；生产必须先停写，并接受“备份间隔内的数据丢失” |

## 5. SLO 与错误预算（建议起点）

| SLI | 建议 SLO | PromQL |
|---|---|---|
| 可用性（非 5xx 比例） | 99.9% / 月（≈43m 不可用） | `1 - sum(rate(http_requests_total{status=~"5.."}[5m])) / sum(rate(http_requests_total[5m]))` |
| 延迟 | p95 < 500ms | `histogram_quantile(0.95, sum by (le)(rate(http_request_duration_seconds_bucket[5m])))` |
| 登录可用性 | 失败率 < 5% | `sum(rate(auth_login_total{result=~"bad_password|unknown_email|error"}[5m])) / sum(rate(auth_login_total[5m]))` |
| 投递成功率 | > 99% | `sum(rate(message_delivery_attempts_total{result="sent"}[1h])) / sum(rate(message_delivery_attempts_total[1h]))` |
| Worker 新鲜度 | message < 15m、housekeeping < 2h | `time() - worker_last_success_timestamp_seconds` |

错误预算烧完（例如 1h 内消耗 20% 月预算）→ 冻结非必要发布，先修可用性。

## 6. 发布前 Checklist

```bash
make ci                    # lint + 测试 + 覆盖率 + 前端门 + 安全机械门
make audit                 # 密钥/日志卫生 + 迁移配对 + 401/403 回归 + govulncheck
make migrate-roundtrip     # 改过迁移 SQL 时必跑（真实 PG，scratch 库）
make verify-auth           # 改过 auth/会话时必跑
make e2e                   # 前端主链路（需后端 + seed 管理员）
make image-scan            # 镜像 HIGH/CRITICAL
make sbom                  # 产出 SBOM（发布存档）
```

- 先 `make migrate-up` 再滚新版本；**回滚代码不要回滚已应用迁移**（迁移必须前向兼容一个版本）。
- 备份：发布前 `make db-backup`。
- 发布后看 `/readyz`、`AppDown`、`High5xxRate`、`MessageWorkerStalled` 四个信号 10 分钟。
