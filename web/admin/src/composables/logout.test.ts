import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import type { AuthUser, LoginResult } from '@/types/auth'

// vi.mock 的工厂会被提升到文件顶部，mock 函数必须用 vi.hoisted 一起提升。
const { logoutMock } = vi.hoisted(() => ({ logoutMock: vi.fn<() => Promise<void>>() }))

// 不打真实后端：这个测试只关心「清态与导航的先后顺序」。
vi.mock('@/api/auth', () => ({
  authApi: { logout: logoutMock },
}))

// 守卫在测试里不应触发真实 refresh 请求；ApiError 等其余导出保留真实实现，
// 否则 notifyError 里的 instanceof 会拿到 undefined。
vi.mock('@/api/http', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/http')>()
  return {
    ...actual,
    refreshSession: vi.fn(async () => false),
    setAuthTokenProvider: vi.fn(),
    setSessionUpdater: vi.fn(),
    setAuthExpiredHandler: vi.fn(),
  }
})

import router from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useLogout } from './useLogout'

const adminUser: AuthUser = {
  id: 1,
  name: 'Admin',
  email: 'admin@example.test',
  role: 'admin',
  permissions: ['users:read', 'dicts:write'],
}

const session = (user: AuthUser): LoginResult => ({
  access_token: 'token',
  token_type: 'Bearer',
  expires_in: 900,
  user,
})

/** useRouter/useRoute 需要组件上下文：app.runWithContext 提供 app 级注入。 */
const withAppContext = async <T>(run: () => Promise<T>): Promise<T> => {
  const app = createApp({ render: () => null })
  app.use(router)
  const result = await app.runWithContext(run)
  return result
}

describe('useLogout', () => {
  beforeEach(async () => {
    logoutMock.mockClear()
    setActivePinia(createPinia())
  })

  it('clears the session before navigating, so /login is not bounced to /dashboard', async () => {
    const auth = useAuthStore()
    auth.setSession(session(adminUser))
    await router.push('/users')
    expect(router.currentRoute.value.path).toBe('/users')

    const order: string[] = []
    logoutMock.mockImplementation(async () => {
      order.push(`logout:${auth.isAuthenticated}`)
      auth.clearAuth()
      order.push(`cleared:${auth.isAuthenticated}`)
    })

    await withAppContext(async () => {
      const { logout } = useLogout()
      await logout()
    })

    // 关键回归：如果先导航再清态，守卫会把 /login 弹回 /dashboard。
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/users')
    expect(auth.isAuthenticated).toBe(false)
    expect(order).toEqual(['logout:true', 'cleared:false'])
  })

  it('still leaves the protected page when the backend logout fails', async () => {
    const auth = useAuthStore()
    auth.setSession(session(adminUser))
    await router.push('/dicts')

    logoutMock.mockImplementation(async () => {
      auth.clearAuth()
      throw new Error('network down')
    })

    await withAppContext(async () => {
      const { logout } = useLogout()
      await logout()
    })

    expect(router.currentRoute.value.path).toBe('/login')
    expect(auth.isAuthenticated).toBe(false)
  })

  it('ignores a second click while the first logout is in flight', async () => {
    const auth = useAuthStore()
    auth.setSession(session(adminUser))
    await router.push('/messages')

    logoutMock.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          auth.clearAuth()
          resolve()
        }),
    )

    await withAppContext(async () => {
      const { logout } = useLogout()
      await Promise.all([logout(), logout()])
    })

    expect(logoutMock).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/login')
  })
})
