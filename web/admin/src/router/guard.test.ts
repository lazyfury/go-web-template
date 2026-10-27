import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { AuthUser, LoginResult } from '@/types/auth'

// 守卫首屏会 bootstrap（refresh cookie → access token）：测试里固定返回「无会话」，
// 其余导出（ApiError/request…）保留真实实现。
vi.mock('@/api/http', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/http')>()
  return {
    ...actual,
    refreshSession: vi.fn(async () => false),
    setAuthTokenProvider: vi.fn(),
    setSessionUpdater: vi.fn(),
  }
})

import router from './index'
import { useAuthStore } from '@/stores/auth'

const adminUser: AuthUser = {
  id: 1,
  name: 'Admin',
  email: 'admin@example.test',
  role: 'admin',
  permissions: ['users:read', 'users:write', 'dicts:read', 'dicts:write', 'messages:read'],
}

const session = (user: AuthUser): LoginResult => ({
  access_token: 'token',
  token_type: 'Bearer',
  expires_in: 900,
  user,
})

const memberUser: AuthUser = {
  id: 2,
  name: 'Member',
  email: 'member@example.test',
  role: 'member',
  permissions: ['users:read:self', 'users:write:self', 'dicts:read', 'messages:read'],
}

/** 每个用例从登录页出发，避免上一个用例停在目标路径导致「重复导航不触发守卫」。 */
const startAtLogin = async (probe: string) => {
  await router.replace({ path: '/login', query: { probe } })
  expect(router.currentRoute.value.path).toBe('/login')
}

describe('router guards', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    localStorage.clear()
    await startAtLogin(`${Date.now()}-${Math.random()}`)
  })

  it('sends an anonymous visitor of a protected page to /login and keeps the target', async () => {
    await router.push('/users?page=2&keyword=a')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/users?page=2&keyword=a')
  })

  it('does not loop when the visitor is already on /login', async () => {
    await router.push({ path: '/login', query: { redirect: '/dicts' } })

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/dicts')
  })

  it('keeps an authenticated visitor away from /login', async () => {
    useAuthStore().setSession(session(adminUser))

    await router.push('/login')

    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it('redirects a page the role cannot open instead of showing an empty screen', async () => {
    useAuthStore().setSession(session(memberUser))

    // 字典管理要求 dicts:write，member 只有 dicts:read。
    await router.push('/dicts')

    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it('lets a permitted role through', async () => {
    useAuthStore().setSession(session(adminUser))

    await router.push('/dicts')

    expect(router.currentRoute.value.path).toBe('/dicts')
  })
})
