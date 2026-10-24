import { request, refreshSession } from './http'
import { useAuthStore } from '@/stores/auth'
import type { AuthUser, ChangePasswordPayload, LoginPayload, LoginResult, Permission } from '@/types/auth'

/**
 * 认证 API —— 真实后端（无 mock）。
 *
 * 令牌策略：
 * - access token 只存在内存（pinia store），刷新页面后由 refresh cookie 重新换回；
 * - refresh token 由后端写入 HttpOnly + SameSite=Strict cookie，前端无法读取（抗 XSS）。
 */

export const authApi = {
  /** 登录：成功后写入 store（access token 内存 + user 持久化）。 */
  async login(payload: LoginPayload): Promise<LoginResult> {
    const result = await request.post<LoginResult>('/auth/login', payload)
    useAuthStore().setSession(result)
    return result
  },

  /**
   * 用 refresh cookie 换新会话。页面刷新后由 store.bootstrap() 调用；
   * 与 http 拦截器共用 single-flight 刷新逻辑。
   */
  async refresh(): Promise<boolean> {
    return refreshSession()
  },

  /** 登出：吊销后端 refresh 会话并清空本地登录态。 */
  async logout(): Promise<void> {
    try {
      await request.post<void>('/auth/logout')
    } finally {
      useAuthStore().clearAuth()
    }
  },

  /** 当前登录用户（含权限）。 */
  async me() {
    return request.get<AuthUser>('/auth/me')
  },

  /** 修改自己的密码（后端会吊销所有会话）。 */
  async changePassword(payload: ChangePasswordPayload): Promise<void> {
    await request.patch<void>('/auth/password', payload)
    useAuthStore().clearAuth()
  },
}

/** 权限判断的便捷导出（给模板/按钮用）。 */
export const hasPermission = (permission: Permission): boolean => useAuthStore().can(permission)
