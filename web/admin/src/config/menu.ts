import type { Component } from 'vue'

/**
 * 侧栏菜单配置 —— 与路由表解耦。
 *
 * 约定：
 * - path 与路由表一一对应（必须是已注册的路由 path）；
 * - icon 使用 UnoCSS 图标类（i-ep-*，按需编译，不会全量打包图标库）；
 * - 新增页面时：注册路由 + 在此追加菜单项。
 */

export interface MenuItem {
  path: string
  title: string
  icon?: string
  children?: MenuItem[]
}

/** 图标组件动态解析：配合 el-icon 使用。 */
export interface MenuItemWithIcon extends MenuItem {
  iconComponent?: Component
}

export const menuConfig: MenuItem[] = [
  {
    path: '/dashboard',
    title: '仪表盘',
    icon: 'i-ep-odometer',
  },
  {
    path: '/users',
    title: '用户管理',
    icon: 'i-ep-user',
  },
]
