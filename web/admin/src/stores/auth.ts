import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { refreshSession, setAuthTokenProvider, setSessionUpdater } from '@/api/http'
import type { AuthUser, LoginResult, Permission } from '@/types/auth'

/**
 * 认证 store —— 登录态的单一事实来源。
 *
 * 安全模型：
 * - access token 只放内存（不写 localStorage）：XSS 无法直接从存储里偷长期令牌；
 * - refresh token 由后端写入 HttpOnly cookie，JS 不可读；
 * - localStorage 只持久化非敏感的用户展示信息（角色/权限据此渲染菜单）；
 * - 首屏或刷新页面后由 bootstrap() 用 cookie 换回 access token。
 *
 * 范式：http 层通过 setAuthTokenProvider / setSessionUpdater 单向回调，store 不反向被 http 依赖。
 */

const USER_KEY = 'gw_admin_user'

const readUser = (): AuthUser | null => {
  const raw = localStorage.getItem(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as AuthUser
  } catch {
    return null
  }
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(null)
  const user = ref<AuthUser | null>(readUser())

  /** 首屏会话恢复只尝试一次（并发调用共享同一个 promise）。 */
  let bootstrapPromise: Promise<void> | null = null
  let sessionChecked = false

  const isAuthenticated = computed(() => Boolean(token.value))
  const permissions = computed<Permission[]>(() => (user.value?.permissions ?? []) as Permission[])

  /** 是否具备某权限（后端 RBAC 的镜像，仅用于 UI 显隐；真正的判定在服务端）。 */
  const can = (permission: Permission): boolean => permissions.value.includes(permission)

  /** 登录/刷新成功后写入会话。 */
  const setSession = (result: LoginResult) => {
    token.value = result.access_token
    user.value = result.user
    localStorage.setItem(USER_KEY, JSON.stringify(result.user))
  }

  /** 登出：清空内存 token 与持久化用户信息。 */
  const clearAuth = () => {
    token.value = null
    user.value = null
    sessionChecked = true
    localStorage.removeItem(USER_KEY)
  }

  /**
   * 首屏会话恢复：refresh cookie 有效则换回 access token。
   * 失败（未登录/已过期）保持未登录态，不抛错。
   */
  const bootstrap = (): Promise<void> => {
    if (sessionChecked) return Promise.resolve()
    if (bootstrapPromise) return bootstrapPromise
    sessionChecked = true
    bootstrapPromise = refreshSession()
      .then(() => undefined)
      .catch(() => undefined)
      .finally(() => {
        bootstrapPromise = null
      })
    return bootstrapPromise
  }

  /** 注册 http 层 token 读取器与刷新回调（main.ts 初始化时调用一次）。 */
  const bindHttpToken = () => {
    setAuthTokenProvider(() => token.value)
    setSessionUpdater((result) => setSession(result))
  }

  return {
    token,
    user,
    permissions,
    isAuthenticated,
    can,
    setSession,
    clearAuth,
    bootstrap,
    bindHttpToken,
  }
})
