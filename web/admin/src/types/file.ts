/**
 * 文件域的类型与展示元数据（与后端 internal/file/dto.go 对齐）。
 *
 * 注意：后端只暴露 id 与 signed URL，存储 key/bucket 不出现在 API 里。
 */

/** 0 = 私有（仅所有者/管理员）；1 = 公开（任意已登录用户可读）。 */
export type FileVisibility = 0 | 1

export interface FileItem {
  id: number
  name: string
  mime_type: string
  ext: string
  size_bytes: number
  biz_type: string
  visibility: FileVisibility
  /** 后端 storage.inline_mime 判定：能否在浏览器内联渲染（图片/PDF/文本/视频）。 */
  inline_allowed: boolean
  sha256: string
  owner_id?: number
  download_path: string
  created_at: string
}

export interface FilePage {
  items: FileItem[]
  total: number
  page: number
  page_size: number
}

export interface FileListParams {
  page?: number
  page_size?: number
  biz_type?: string
  keyword?: string
}

export interface FileURL {
  url: string
  expires_at: string
}

/**
 * 长期公开资产链接（后端 file_shares）。
 * 与 FileURL 的区别：token 可撤销、可永久有效，适合文章封面/头像；
 * 短签名 URL 用于私有文件临时下载。
 */
/** 纯文本预览（GET /api/v1/files/:id/text，后端截断到 256 KiB）。 */
export interface TextPreview {
  content: string
  truncated: boolean
  size_bytes: number
}

export interface AssetLink {
  url: string
  purpose: string
  expires_at?: string
}

/** 下载方式：只有白名单类型允许 inline 渲染。 */
export type FileDisposition = 'inline' | 'attachment'

export const FileVisibilityMeta: Record<FileVisibility, { label: string; tagType: 'info' | 'success' }> = {
  0: { label: '私有', tagType: 'info' },
  1: { label: '公开', tagType: 'success' },
}

/** 常用业务类型（后端只做格式校验，取值由业务方约定）。 */
export const FileBizTypeOptions = [
  { value: 'avatar', label: '头像' },
  { value: 'attachment', label: '附件' },
  { value: 'image', label: '图片' },
  { value: 'export', label: '导出文件' },
]

/** 与后端 storage.max_upload_bytes 的默认值保持一致（仅用于前端提前拦截）。 */
export const DefaultMaxUploadBytes = 10 * 1024 * 1024

/** 默认允许的扩展名（后端 storage.allowed_ext 是权威）。 */
export const DefaultAllowedExt = [
  '.png',
  '.jpg',
  '.jpeg',
  '.webp',
  '.gif',
  '.pdf',
  '.txt',
  '.zip',
  '.docx',
  '.xlsx',
]

/** 人类可读的文件大小。 */
export const formatFileSize = (bytes: number): string => {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value >= 10 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`
}

/** 从文件名取小写扩展名（含点）。 */
export const fileExt = (name: string): string => {
  const index = name.lastIndexOf('.')
  return index >= 0 ? name.slice(index).toLowerCase() : ''
}
