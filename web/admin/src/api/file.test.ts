import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * 文件域 API 的请求形状测试：上传必须走 FormData（multipart），下载/外链必须
 * 带上 disposition/purpose。这些字段错了前端不一定报错，只会在浏览器里静默变成
 * 附件下载或永久链接失效，所以在这里钉死。
 */
const request = {
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  delete: vi.fn(),
}

vi.mock('./http', () => ({ request }))

const { fileApi } = await import('./file')

beforeEach(() => {
  vi.clearAllMocks()
})

describe('fileApi', () => {
  it('forwards list filters', async () => {
    request.get.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })

    await fileApi.list({ page: 2, page_size: 10, keyword: 'logo', biz_type: 'avatar' })

    expect(request.get).toHaveBeenCalledWith('/files', {
      params: { page: 2, page_size: 10, keyword: 'logo', biz_type: 'avatar' },
    })
  })

  it('uploads multipart with optional fields and reports progress', async () => {
    request.post.mockResolvedValue({ id: 1 })
    const progress: number[] = []
    const file = new File(['x'], 'logo.png', { type: 'image/png' })

    await fileApi.upload(file, {
      bizType: 'avatar',
      visibility: 1,
      onProgress: (value) => progress.push(value),
    })

    const [url, body, config] = request.post.mock.calls[0]
    expect(url).toBe('/files')
    expect(body).toBeInstanceOf(FormData)
    const form = body as FormData
    expect(form.get('file')).toBe(file)
    expect(form.get('biz_type')).toBe('avatar')
    expect(form.get('visibility')).toBe('1')
    expect(config.headers).toEqual({ 'Content-Type': 'multipart/form-data' })

    config.onUploadProgress({ loaded: 5, total: 10 })
    config.onUploadProgress({ loaded: 9, total: 10 })
    // unknown total: no bogus 0%/NaN in the UI
    config.onUploadProgress({ loaded: 3 })
    expect(progress).toEqual([50, 90])
  })

  it('omits empty optional upload fields', async () => {
    request.post.mockResolvedValue({ id: 1 })
    await fileApi.upload(new File(['x'], 'a.txt'))

    const form = request.post.mock.calls[0][1] as FormData
    expect(form.get('biz_type')).toBeNull()
    expect(form.get('visibility')).toBeNull()
  })

  it('requests signed URLs with disposition and ttl', async () => {
    request.get.mockResolvedValue({ url: 'http://x/files/1', expires_at: 'now' })

    await fileApi.url(7, 'inline', 120)

    expect(request.get).toHaveBeenCalledWith('/files/7/url', {
      params: { disposition: 'inline', ttl_seconds: 120 },
    })
  })

  it('creates and revokes long-lived shares', async () => {
    request.post.mockResolvedValue({ url: 'http://x/a/token/logo.png', purpose: 'asset' })
    request.delete.mockResolvedValue(undefined)

    await fileApi.share(7, { purpose: 'article-cover', expiresInSeconds: 3600 })
    expect(request.post).toHaveBeenCalledWith('/files/7/share', {
      purpose: 'article-cover',
      expires_in_seconds: 3600,
    })

    // Defaults must not send empty fields (the backend treats omissions as
    // "asset purpose, permanent").
    await fileApi.share(7)
    expect(request.post).toHaveBeenLastCalledWith('/files/7/share', {
      purpose: undefined,
      expires_in_seconds: undefined,
    })

    await fileApi.revokeShare(7, 'article-cover')
    expect(request.delete).toHaveBeenCalledWith('/files/7/share', {
      params: { purpose: 'article-cover' },
    })
    await fileApi.revokeShare(7)
    expect(request.delete).toHaveBeenLastCalledWith('/files/7/share', {
      params: { purpose: undefined },
    })
  })

  it('delegates metadata operations', async () => {
    request.get.mockResolvedValue({ id: 7 })
    request.patch.mockResolvedValue({ id: 7, visibility: 0 })
    request.delete.mockResolvedValue(undefined)

    await fileApi.get(7)
    expect(request.get).toHaveBeenCalledWith('/files/7')

    // 0 must survive: switching back to private is a real request.
    await fileApi.updateVisibility(7, 0)
    expect(request.patch).toHaveBeenCalledWith('/files/7', { visibility: 0 })

    await fileApi.remove(7)
    expect(request.delete).toHaveBeenCalledWith('/files/7')
  })
})
