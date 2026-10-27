import { describe, expect, it } from 'vitest'
import type { RouteRecordRaw } from 'vue-router'
import { menuConfig, type MenuItem } from './menu'
import { routes } from '@/router/routes'

const flattenRoutes = (records: RouteRecordRaw[], parent = ''): Map<string, RouteRecordRaw> => {
  const map = new Map<string, RouteRecordRaw>()
  for (const record of records) {
    const path = record.path.startsWith('/') ? record.path : `${parent}/${record.path}`.replace(/\/+/g, '/')
    map.set(path, record)
    if (record.children) {
      for (const [childPath, child] of flattenRoutes(record.children, path)) {
        map.set(childPath, child)
      }
    }
  }
  return map
}

const leafMenus = (items: MenuItem[], inherited?: MenuItem['permission']): Array<{ item: MenuItem; permission?: MenuItem['permission'] }> =>
  items.flatMap((item) => {
    const permission = item.permission ?? inherited
    if (item.children?.length) return leafMenus(item.children, permission)
    return [{ item, permission }]
  })

describe('menu ↔ routes consistency', () => {
  const routeMap = flattenRoutes(routes)

  it('only links to routes that exist', () => {
    for (const { item } of leafMenus(menuConfig)) {
      expect(routeMap.has(item.path), `menu path ${item.path} has no route`).toBe(true)
    }
  })

  it('guards every page with the same permission as its route', () => {
    for (const { item, permission } of leafMenus(menuConfig)) {
      const route = routeMap.get(item.path)
      if (!route) continue
      expect(route.meta?.permission, `permission drift for ${item.path}`).toBe(permission)
      expect(route.meta?.requiresAuth, `menu page ${item.path} must require auth`).not.toBe(false)
    }
  })

  it('does not hide a route from the sidebar by accident', () => {
    // Every permission-guarded page under the admin layout should be reachable
    // from the menu, otherwise a user with the permission cannot navigate.
    const menuPaths = new Set(leafMenus(menuConfig).map(({ item }) => item.path))
    for (const [path, route] of routeMap) {
      if (path === '/login' || path.startsWith('/:') || path === '/') continue
      if (!route.meta?.permission) continue
      expect(menuPaths.has(path), `guarded route ${path} is missing from the menu`).toBe(true)
    }
  })
})
