import type { Component } from 'vue'
import { Collection, Odometer, Search, Setting, Tools, User } from '@element-plus/icons-vue'
import type { PermissionKey } from '@/router/routes'

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
  /** 需要的权限；缺省表示登录即可见（与后端 RBAC 对齐）。 */
  permission?: PermissionKey
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
    // 只有能读用户列表的角色（admin）才显示；member 只能看自己的资料。
    permission: 'users:read',
  },
  {
    path: '/dicts',
    title: '字典管理',
    icon: Collection,
    // 管理端需要写权限；消费侧读取由各业务页面自行调用 /dicts/:group。
    permission: 'dicts:write',
  },
  {
    path: '/settings',
    title: '系统设置',
    icon: Setting,
    permission: 'dicts:write',
    children: [
      { path: '/settings/basic', title: '基础设置', icon: Tools },
      { path: '/settings/seo', title: 'SEO设置', icon: Search },
    ],
  },
]
