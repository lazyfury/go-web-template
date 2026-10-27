# 可观测性落地物

模板自带 Prometheus 指标（`/metrics`）、结构化访问日志与 `/readyz`，但「指标 + 看板 + 告警」
三件套才算闭环。这里放的是与代码同步维护的两份落地物：

| 文件 | 用途 |
|---|---|
| `prometheus/alerts.yml` | 告警规则（可用 `rule_files` 直接加载，或包成 prometheus-operator 的 `PrometheusRule`） |
| `grafana/dashboard.json` | Grafana 看板（导入时选择 Prometheus 数据源；uid 固定为 `go-web-template`） |

## 1. 抓取配置

`/metrics` 默认开启，但 `deploy/docker-compose.yml`（生产式编排）默认 `APP_METRICS_ENABLED=false`：
它会暴露 Go 版本、路由清单与流量画像，生产应经内网/反代 ACL 抓取，而不是公网直出。

```yaml
# prometheus.yml
scrape_configs:
  - job_name: go-web-template
    metrics_path: /metrics
    static_configs:
      - targets: ["go-web-template.internal:8080"]
    # 多副本时用服务发现替代 static_configs，up{job=...} 告警按实例触发。
```

需要 Prometheus 直接抓编排里的容器时，用 `APP_METRICS_ENABLED=true make docker-up`（只建议本地）。

## 2. 告警

```yaml
# prometheus.yml
rule_files:
  - deploy/prometheus/alerts.yml
```

规则分组：

| 组 | 规则 | 触发条件（模板默认） |
|---|---|---|
| availability | `AppDown` | `up{job="go-web-template"}==0` 持续 2m |
| availability | `High5xxRate` | 5xx 占比 > 5% 持续 10m |
| availability | `HighLatencyP95` | p95 > 1s 持续 10m |
| auth | `RefreshTokenReuseDetected` | 15m 内出现 `auth_refresh_total{result="reuse"}` |
| auth | `LoginFailureSpike` | 失败登录 > 5/s 持续 10m |
| auth | `AccountLockoutSpike` | 15m 内 > 3 次账号锁定 |
| message | `DeliveryDead` | 15m 内出现 `result="dead"` |
| message | `DeliveryFailureRatio` | 失败率 > 20% 持续 10m |
| message | `DeliveryLeaseLost` | 15m 内 > 5 次 `lease_lost`（重复投递风险） |
| operations | `HousekeepingTaskFailing` | 1h 内出现清理任务失败 |
| operations | `RateLimitStoreErrors` | 5m 内出现 Redis 限流存储错误（已 fail-open） |

阈值是起点：先按自己的流量基线调 `for` 与阈值，再谈「告警不吵」。

## 3. 看板

`grafana/dashboard.json` 可直接导入（Dashboards → Import → 选择文件 → 绑 Prometheus 数据源）。
面板：请求速率/状态码、p50/p95/p99、在途请求、登录与刷新结果、会话吊销原因、投递结果与耗时、
保留期删除量。所有 label 都是闭集（`result`/`reason`/`task`/`channel`/`path` 路由模板），
不含用户/邮箱/目标地址，所以不需要担心 PII 或高基数。

## 4. 与代码同步

指标名/label 由代码决定，改 `internal/*/metrics.go` 或 `internal/web/middleware_metrics.go` 时：

1. 同步本目录的两个文件（名字拼错 = 面板空白、告警永不触发）；
2. 在对应 `*_test.go` 里断言计数（见 `internal/auth/metrics_test.go`、`internal/message/worker_test.go`），
   否则「指标发出来了但 label 值变了」没人发现。
