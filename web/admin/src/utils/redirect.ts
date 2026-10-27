/**
 * 把 URL 里的 `redirect` 参数收敛成安全的站内路径。
 *
 * 登录页与「登录过期」共用：只接受单个 `/` 开头的站内绝对路径，
 * 拒绝协议相对（`//evil.com`）、外部 URL、回到登录页本身（会形成循环）。
 */
const LOGIN_PATHS = ['/login', '/login?', '/login#']

export const safeRedirectTarget = (raw: unknown, fallback = '/dashboard'): string => {
  if (typeof raw !== 'string') return fallback
  const target = raw.trim()
  if (!target.startsWith('/')) return fallback
  // 协议相对地址（//host）与反斜杠变体（/\host）在浏览器里会被当作外部主机。
  if (target.startsWith('//') || target.startsWith('/\\')) return fallback
  // 控制字符/换行会被部分客户端截断，直接丢弃。
  for (const char of target) {
    const code = char.codePointAt(0) ?? 0
    if (code < 0x20 || code === 0x7f) return fallback
  }
  if (LOGIN_PATHS.some((prefix) => target.startsWith(prefix))) return fallback
  return target
}
