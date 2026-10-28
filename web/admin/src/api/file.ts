import { request } from './http'
import type {
  AssetLink,
  TextPreview,
  FileDisposition,
  FileItem,
  FileListParams,
  FilePage,
  FileURL,
  FileVisibility,
} from '@/types/file'

/**
 * 文件域 API。
 *
 * 上传走 multipart（服务端流式落盘/落对象存储并嗅探真实类型），
 * 下载统一先换签名 URL：浏览器/移动端直接请求存储，API 不代理字节。
 */
export const fileApi = {
  /** GET /api/v1/files?page&page_size&biz_type&keyword */
  list(params: FileListParams): Promise<FilePage> {
    return request.get<FilePage>('/files', { params })
  },

  /** GET /api/v1/files/:id */
  get(id: number): Promise<FileItem> {
    return request.get<FileItem>(`/files/${id}`)
  },

  /** POST /api/v1/files（multipart/form-data） */
  upload(
    file: File,
    options: {
      bizType?: string
      visibility?: FileVisibility
      onProgress?: (percent: number) => void
    } = {},
  ): Promise<FileItem> {
    const form = new FormData()
    form.append('file', file)
    if (options.bizType) form.append('biz_type', options.bizType)
    if (options.visibility !== undefined) form.append('visibility', String(options.visibility))

    return request.post<FileItem>('/files', form, {
      // 交给浏览器生成 boundary；不要手工设置 Content-Type。
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 120000,
      onUploadProgress: (event) => {
        if (!options.onProgress || !event.total) return
        options.onProgress(Math.min(100, Math.round((event.loaded / event.total) * 100)))
      },
    })
  },

  /**
   * GET /api/v1/files/:id/text：text/* 的有界纯文本预览。
   * 走 JSON 而不是直接 iframe 签名 URL：后者受 Content-Disposition/CORS 限制，
   * 而且预览不应该把文件内容暴露成可缓存的公开 URL。
   */
  previewText(id: number): Promise<TextPreview> {
    return request.get<TextPreview>(`/files/${id}/text`)
  },

  /** GET /api/v1/files/:id/url?disposition&ttl_seconds */
  url(id: number, disposition: FileDisposition = 'attachment', ttlSeconds = 600): Promise<FileURL> {
    return request.get<FileURL>(`/files/${id}/url`, {
      params: { disposition, ttl_seconds: ttlSeconds },
    })
  },

  /** PATCH /api/v1/files/:id（仅所有者/管理员） */
  updateVisibility(id: number, visibility: FileVisibility): Promise<FileItem> {
    return request.patch<FileItem>(`/files/${id}`, { visibility })
  },

  /**
   * POST /api/v1/files/:id/share：创建/复用长期公开链接（owner/admin）。
   * 同 (file, purpose) 幂等：重复调用返回同一个 URL，已缓存的 HTML 不会失效。
   */
  share(id: number, options: { purpose?: string; expiresInSeconds?: number } = {}): Promise<AssetLink> {
    return request.post<AssetLink>(`/files/${id}/share`, {
      purpose: options.purpose || undefined,
      expires_in_seconds: options.expiresInSeconds || undefined,
    })
  },

  /** DELETE /api/v1/files/:id/share（purpose 为空 = 撤销全部） */
  revokeShare(id: number, purpose?: string): Promise<void> {
    return request.delete<void>(`/files/${id}/share`, { params: { purpose: purpose || undefined } })
  },

  /** DELETE /api/v1/files/:id（软删，blob 由后台任务清理） */
  remove(id: number): Promise<void> {
    return request.delete<void>(`/files/${id}`)
  },
}
