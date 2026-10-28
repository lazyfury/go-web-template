import { request } from './http'
import type { FileDisposition, FileItem, FileListParams, FilePage, FileURL, FileVisibility } from '@/types/file'

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

  /** DELETE /api/v1/files/:id（软删，blob 由后台任务清理） */
  remove(id: number): Promise<void> {
    return request.delete<void>(`/files/${id}`)
  },
}
