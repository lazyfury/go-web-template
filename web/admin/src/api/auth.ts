import { request } from './http'
import { useAuthStore } from '@/stores/auth'
import type { LoginPayload, LoginResult } from '@/types/auth'

/**
 * 认证 API。
 *
 * go-web-template 后端当前未提供认证接口，默认走本地 mock（USE_MOCK = true），
 * 保证模板开箱即用。接入真实后端时：
 *   1. 将 USE_MOCK 置为 false；
 *   2. 按后端契约调整 loginByPassword 的请求路径与响应字段；
 *   3. 如需拉取用户资料/权限，在下方追加对应方法。
 */
const USE_MOCK = true

const MOCK_DELAY = 500

/** 本地 mock：任意非空用户名/密码即登录成功（演示用）。 */
const mockLogin = async (payload: LoginPayload): Promise<LoginResult> => {
  await new Promise((resolve) => setTimeout(resolve, MOCK_DELAY))
  const name = payload.username.trim()
  if (!name || !payload.password) {
    throw new Error('请输入用户名和密码')
  }
  return {
    token: `mock-token-${Date.now()}`,
    user: {
      id: 1,
      username: name,
      email: `${name}@admin.local`,
      role: '管理员',
    },
  }
}

/** 真实接口预留实现：POST /auth/login。 */
const realLogin = async (payload: LoginPayload): Promise<LoginResult> => {
  return request.post<LoginResult>('/auth/login', payload)
}

export const authApi = {
  /** 登录：成功后将 token 与用户写入 auth store（含持久化）。 */
  async login(payload: LoginPayload): Promise<LoginResult> {
    const result = USE_MOCK ? await mockLogin(payload) : await realLogin(payload)

    const authStore = useAuthStore()
    authStore.setAuth(result.token, result.user)
    return result
  },

  /** 登出：清空本地登录态（后端无接口，如需后端吊销 token 在此追加）。 */
  logout(): void {
    useAuthStore().clearAuth()
  },
}
