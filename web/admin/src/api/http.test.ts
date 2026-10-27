import { describe, expect, it } from 'vitest'
import { shouldRefreshSession } from './http'

describe('shouldRefreshSession', () => {
  it('refreshes and replays a protected request exactly once', () => {
    expect(shouldRefreshSession('/users/1', false)).toBe(true)
    expect(shouldRefreshSession('/users/1', true)).toBe(false)
    expect(shouldRefreshSession(undefined, false)).toBe(true)
  })

  it('treats auth endpoints 401 as business results, not expired sessions', () => {
    // /auth/password answers 401 when the *current* password is wrong: refreshing
    // and then redirecting to /login would log the user out over a typo.
    for (const url of ['/auth/login', '/auth/refresh', '/auth/logout', '/auth/password']) {
      expect(shouldRefreshSession(url, false)).toBe(false)
    }
  })

  it('does not confuse a lookalike path with an auth endpoint', () => {
    expect(shouldRefreshSession('/auth/passwordless-login', false)).toBe(true)
    expect(shouldRefreshSession('/users/auth/password', false)).toBe(true)
  })
})
