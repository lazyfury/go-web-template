import type { Component } from 'vue'
import { Odometer, User } from '@element-plus/icons-vue'

/**
 * 侧栏菜单配置 —— 与路由表解耦。
 *
 * 约定：
 * - path 与路由表一一对应（必须是已注册的路由 path）；
 * - icon 使用 @element-plus/icons-vue 的图标**组件**（按需 import，tree-shaking 生效）；
 *   Element Plus 菜单折叠态只保留 el-icon 包裹的图标，因此必须配合 el-icon 使用
 *   （见 AppSidebar.vue），不能用裸 span + UnoCSS 类，否则收起时图标被隐藏。
 */

export interface MenuItem {
  path: string
  title: string
  icon?: Component
  children?: MenuItem[]
}

export const menuConfig: MenuItem[] = [
  {
    path: '/dashboard',
    title: '仪表盘',
    icon: Odometer,
  },
  {
    path: '/users',
    title: '用户管理',
    icon: User,
  },
]
