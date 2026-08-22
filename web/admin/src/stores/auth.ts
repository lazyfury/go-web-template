import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { setAuthTokenProvider } from '@/api/http'
import type { AuthUser } from '@/types/auth'

/**
 * 认证 store —— 登录态的单一事实来源。
 *
 * 范式说明（相对参考项目的修正）：
 * - 状态只存在 pinia store 中，localStorage 仅作为持久化介质（由 action 显式读写）；
 *   不再出现「store 与 localStorage 双轨、按调用方环境回退」的写法。
 * - http 层通过 setAuthTokenProvider 注入读取函数，store 与 http 之间保持单向依赖。
 */

const TOKEN_KEY = 'gw_admin_token'
const USER_KEY = 'gw_admin_user'

const readToken = (): string | null => localStorage.getItem(TOKEN_KEY)
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
  const token = ref<string | null>(readToken())
  const user = ref<AuthUser | null>(readUser())

  const isAuthenticated = computed(() => Boolean(token.value))

  /** 登录：写入 store 并持久化。 */
  const setAuth = (nextToken: string, nextUser: AuthUser) => {
    token.value = nextToken
    user.value = nextUser
    localStorage.setItem(TOKEN_KEY, nextToken)
    localStorage.setItem(USER_KEY, JSON.stringify(nextUser))
  }

  /** 登出：清空 store 与持久化。 */
  const clearAuth = () => {
    token.value = null
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  }

  /** 注册 http 层 token 读取器（初始化时调用一次即可）。 */
  const bindHttpToken = () => {
    setAuthTokenProvider(() => token.value)
  }

  return {
    token,
    user,
    isAuthenticated,
    setAuth,
    clearAuth,
    bindHttpToken,
  }
})
