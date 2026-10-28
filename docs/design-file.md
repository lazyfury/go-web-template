# 文件管理模块（files）设计

> 状态：Phase 1–2 已实现（本地驱动 + S3 兼容驱动 + 签名下载 + 前端页面）。
> 相关代码：`internal/file/`（业务域）、`internal/platform/blobx/`（存储抽象）、
> `migrations/000010_create_file_tables.*`、`web/admin/src/views/FilesView.vue`。

## 1. 范围

**做**：单文件上传（multipart 流式写入）、元数据管理、签名下载 URL（本地 HMAC / S3 预签名）、
可见性、跨模块挂载（`file_links` + `Binder`）、软删 + 异步清理、保留期任务、前端文件管理页。

**不做（本期）**：分片/断点续传、浏览器直传 OSS（需要临时凭证交换，见 §9）、秒传（sha256 已落库，接口未开）、
图片处理/缩略图、病毒扫描、公开（匿名）下载。

## 2. 分层

```
internal/platform/blobx       叶子包：Store 接口 + local/s3 驱动 + 签名/预签名
        ▲
internal/file                 业务域：entity/repository/service/handler/routes + Binder
        ▲
internal/app                  组合根：blobx.New → file.NewService → RegisterRoutes
```

- `blobx` 不认识数据库、gin、业务错误码；只负责「按 key 存取字节 + 生成下载地址」。
- `file` 不认识具体驱动：只持有 `blobx.Store`，行里记录 `storage` 驱动名。
- 其他业务域**不 import `internal/file` 的 repository**，只用消费侧窄接口（见 §6）。

## 3. 数据模型

`files`（元数据）：

| 列 | 说明 |
|---|---|
| `storage` / `bucket` / `object_key` | 对象定位三元组；key 由服务端生成 `file/YYYYMM/<uuid><ext>` |
| `original_name` / `ext` / `mime_type` | 展示名（清洗）、扩展名、**服务端嗅探**的 MIME |
| `size_bytes` / `sha256` | 实际写入字节数、流式哈希（秒传/取证锚点） |
| `biz_type` | 业务类型（avatar/attachment/export…），只用于分组与展示 |
| `visibility` | 0=私有（默认）1=公开（任意登录用户可读） |
| `status` | 0=pending 1=ready 2=deleting |
| `owner_id` | 上传者（`users` 软删时置 NULL） |
| `deleted_at` | 软删时间；查询一律 `deleted_at IS NULL`（**显式过滤**，不依赖 GORM 的 `*time.Time` 软删语义） |

`file_links`（跨模块引用）：`(file_id, owner_type, owner_id, slot)` + 软删；部分唯一索引保证重复挂载幂等。

- `owner_type` 由业务方定义（`user.avatar`、`message.attachment`、`post.cover`），新模块接入不需要迁移。
- 不做跨域外键：业务表软删顺序不可控，硬外键只会把删除变成竞态。

## 4. 上传流程

1. 路由级 `BodyLimit` 把上限放宽到 `storage.max_upload_bytes + 1 MiB`（全局 JSON 仍是 1 MiB）。
2. `ParseMultipartForm(max_upload_bytes)`：上限内全部在内存，不会落临时盘。
3. 服务层依次校验：文件名清洗 → 扩展名白名单 → `biz_type` 格式 → 大小上限 → **前 512B 嗅探 MIME** 并在白名单内
   （`application/octet-stream`、`text/html`、`image/svg+xml` 直接拒绝）。
4. 生成 key → `io.TeeReader(sha256)` 流式写入驱动 → 元数据落库。
5. 落库失败 → 尽力删除刚写入的 blob（孤儿对象无法被 janitor 定位，比丢一行更糟）。

## 5. 下载流程

- `GET /api/v1/files/:id/url?disposition=inline|attachment&ttl_seconds=` 返回短期 URL。
- `inline` 只对 `storage.inline_mime` 生效，其余强制 `attachment`（上传的 HTML/SVG 内联渲染 = API 域上的存储型 XSS）。
- 本地驱动：`GET /files/{id}/{disposition}/{exp}/{sig}`，HMAC-SHA256(key|exp|disposition)。
  - 签名错/过期/文件已删 → **404**（不确认 id 是否存在）。
  - 路由不在 `/api/v1` 下：签名即凭证，因此不违反「`/api/v1` 默认拒绝」。
  - 命中 `*os.File` 时走 `http.ServeContent`，支持 Range（PDF/视频拖动）。
- S3 驱动：预签名 GET（`response-content-disposition` 带原始文件名），浏览器直连存储，API 不代理字节。

## 6. 其他模块如何引用文件

```go
// 消费侧声明窄接口（不要 import internal/file 的 repository）
type AvatarFiles interface {
    Attach(ctx context.Context, actor authkit.Principal, in file.AttachInput) (*file.File, error)
    ListByOwner(ctx context.Context, ownerType string, ownerID uint64) ([]file.File, error)
    DeleteByOwner(ctx context.Context, ownerType string, ownerID uint64) error
    SignedURL(ctx context.Context, fileID uint64, disposition string) (file.URLResult, error)
}

// 组合根注入 *file.Service（internal/app）
userService := user.NewService(..., fileBinder)
```

规则：

- 多附件 / 数量不定 → `file_links`（`slot=""`）。
- 单值槽位（头像、封面）→ `file_links`，`slot="avatar"`；不要直接把 URL 存进业务表（URL 会过期、换驱动即失效）。
- 资源删除时调用 `DeleteByOwner`；未被引用的文件由 janitor 在 `storage.orphan_ttl` 后清理。
- `SignedURL` 是**可信内部 API**（无 actor 校验），只能用于「服务端已经完成授权」的场景，不能接用户传来的 id。

## 7. 清理与保留

`internal/housekeeping` 追加两个任务（由 `cmd/server` 启动，复用 ADR 14 的生命周期）：

| 任务 | 动作 |
|---|---|
| `files_orphans` | `ready` 且创建于 `now - storage.orphan_ttl` 之前、无有效 link → 置 `deleting` |
| `files_purge` | `deleting` → 先删 blob，再硬删行（失败保留行，下一轮重试） |

顺序不能反：先删行就再也定位不到 blob。`retention.enabled=false` 时两个任务都不跑，
删除只会软删，blob 会堆积（配置项里明确写了这一点）。

## 8. 安全清单（实现位置）

| 风险 | 处理 |
|---|---|
| 客户端伪造 Content-Type | 服务端 `http.DetectContentType` 嗅探 + 白名单（`service.go`） |
| 路径穿越 / 文件名注入 | `cleanFilename` + 服务端生成 key + `os.Root`（`blobx/local.go`） |
| 存储型 XSS（HTML/SVG 内联） | 类型拒绝 + `inline_mime` 白名单 + `Content-Disposition` + `nosniff` |
| 越权读/删 | `files:read/write` 之后 service 内做归属校验；列表强制 owner 过滤 |
| 匿名遍历 | 无匿名 API；下载只认 HMAC/预签名（短 TTL，默认 10m，上限 1h） |
| 资源耗损 | 单文件上限 + 路由级 BodyLimit + 上传/下载独立限流桶 |
| 日志/指标泄漏 | 只记 id/size/mime；MIME 归并为闭集标签；不记文件名与签名 URL |
| 弱签名密钥 | `storage.signing_secret` 生产要求 ≥32 字节且非占位值（与 JWT 同规则） |

## 9. 待办 / 已知取舍

1. **浏览器直传 OSS**：需要「临时凭证交换」端点（STS/S3 presigned PUT + 回调确认）。
   一旦直传，服务端就无法嗅探真实类型，需要 `HEAD` 校验 + 上传后异步检查；本期未做，仍走服务端中转。
2. **秒传/去重**：`sha256 + size_bytes` 已建索引，接口未开放（跨用户去重会泄露「这个文件已存在」）。
3. **本地驱动的多副本**：需要共享卷（NFS/EFS）；否则请用 `storage.driver=s3`。
4. **签名密钥轮换**：换 `storage.signing_secret` 会让已发出的下载链接立即失效（TTL 内）——
   这是特性不是 bug；S3 驱动不受影响（预签名由存储侧签名）。
5. **MIME 交叉校验**：目前只校验嗅探结果，不看扩展名与 MIME 是否匹配（如 `.png` + `application/pdf` 会被拒，
   但 `.zip` + docx 内容会通过）—— 需要更严格的业务策略时在 service 加映射表。

## 10. 验收

`make check`（含 lint/race/覆盖率/OpenAPI 黄金文件）、`make audit`、`make migrate-roundtrip`、
有 PG 时 `make smoke`（包含上传 → 签名下载 → 删除 → 404 的真实进程链路）。
前端：`make web-check`（eslint + vue-tsc + vitest + api:check + build）。
