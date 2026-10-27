import { describe, expect, it } from 'vitest'
import { safeRedirectTarget } from './redirect'

describe('safeRedirectTarget', () => {
  it('keeps internal paths, including query and hash', () => {
    expect(safeRedirectTarget('/users?page=2&keyword=a#top')).toBe('/users?page=2&keyword=a#top')
    expect(safeRedirectTarget('/settings/seo')).toBe('/settings/seo')
  })

  it('falls back for external or protocol-relative targets', () => {
    expect(safeRedirectTarget('https://evil.example')).toBe('/dashboard')
    expect(safeRedirectTarget('//evil.example/path')).toBe('/dashboard')
    expect(safeRedirectTarget('/\\evil.example')).toBe('/dashboard')
    expect(safeRedirectTarget('javascript:alert(1)')).toBe('/dashboard')
  })

  it('never redirects back to the login page', () => {
    expect(safeRedirectTarget('/login')).toBe('/dashboard')
    expect(safeRedirectTarget('/login?redirect=/users')).toBe('/dashboard')
  })

  it('falls back for junk input', () => {
    expect(safeRedirectTarget(undefined)).toBe('/dashboard')
    expect(safeRedirectTarget(null)).toBe('/dashboard')
    expect(safeRedirectTarget('')).toBe('/dashboard')
    expect(safeRedirectTarget('users')).toBe('/dashboard')
    expect(safeRedirectTarget('/users\nSet-Cookie: x')).toBe('/dashboard')
    expect(safeRedirectTarget(['/users'])).toBe('/dashboard')
  })

  it('honours a custom fallback', () => {
    expect(safeRedirectTarget(undefined, '/')).toBe('/')
  })
})
