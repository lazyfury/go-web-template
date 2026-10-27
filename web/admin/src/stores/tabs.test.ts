import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useTabsStore } from './tabs'

const TABS_KEY = 'gw_admin_tabs'

describe('tabs store', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('starts empty when storage holds junk', () => {
    localStorage.setItem(TABS_KEY, 'not json')
    expect(useTabsStore().tabs).toEqual([])

    localStorage.setItem(TABS_KEY, JSON.stringify([{ path: 1 }, { path: '/users', title: '用户', closable: true }]))
    // 上一个 store 已被 pinia 缓存，重新建一个才会重新读存储。
    setActivePinia(createPinia())
    const store = useTabsStore()
    expect(store.tabs.map((tab) => tab.path)).toEqual(['/users'])
  })

  it('keeps the dashboard tab first and only once', () => {
    const store = useTabsStore()
    store.ensureDashboard()
    store.ensureDashboard()

    expect(store.tabs[0]).toEqual({ path: '/dashboard', title: '仪表盘', closable: false })
    expect(store.tabs.filter((tab) => tab.path === '/dashboard')).toHaveLength(1)
    expect(JSON.parse(localStorage.getItem(TABS_KEY) ?? '[]')).toHaveLength(1)
  })

  it('dedupes by path when opening tabs', () => {
    const store = useTabsStore()
    store.ensureDashboard()
    store.upsert({ path: '/users', title: '用户管理', closable: true })
    store.upsert({ path: '/users', title: '用户管理（重复）', closable: true })

    expect(store.tabs.map((tab) => tab.path)).toEqual(['/dashboard', '/users'])
    expect(store.tabs[1].title).toBe('用户管理')
  })

  it('closes others but keeps the dashboard and the active tab', () => {
    const store = useTabsStore()
    store.ensureDashboard()
    store.upsert({ path: '/users', title: '用户管理', closable: true })
    store.upsert({ path: '/dicts', title: '字典管理', closable: true })

    store.closeOthers('/dicts')

    expect(store.tabs.map((tab) => tab.path)).toEqual(['/dashboard', '/dicts'])
    expect(JSON.parse(localStorage.getItem(TABS_KEY) ?? '[]')).toHaveLength(2)
  })

  it('removes one tab and resets to the dashboard', () => {
    const store = useTabsStore()
    store.ensureDashboard()
    store.upsert({ path: '/users', title: '用户管理', closable: true })

    expect(store.remove('/users')).toBe('/users')
    expect(store.tabs.map((tab) => tab.path)).toEqual(['/dashboard'])

    store.closeAll()
    expect(store.tabs.map((tab) => tab.path)).toEqual(['/dashboard'])
  })
})
