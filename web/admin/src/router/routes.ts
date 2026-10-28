import type { RouteRecordRaw } from 'vue-router'
/**
 * 路由/菜单用到的权限标识（与后端 authkit.Permission 对齐）。
 * 集中在这里，避免在模板/守卫里散落裸字符串。
 */
export type PermissionKey =
  | 'users:read'
  | 'users:write'
  | 'users:delete'
  | 'users:read:self'
  | 'users:write:self'
  | 'dicts:read'
  | 'dicts:write'
  | 'messages:read'
  | 'messages:send'
  | 'messages:manage'
  | 'files:read'
  | 'files:write'
import AdminLayout from '@/layouts/AdminLayout.vue'
import LoginView from '@/views/LoginView.vue'
import DashboardView from '@/views/DashboardView.vue'
import UsersView from '@/views/UsersView.vue'
import DictsView from '@/views/DictsView.vue'
import FilesView from '@/views/FilesView.vue'
import BasicSettingsView from '@/views/BasicSettingsView.vue'
import SeoSettingsView from '@/views/SeoSettingsView.vue'
import MessagesView from '@/views/MessagesView.vue'
import SendMessageView from '@/views/SendMessageView.vue'
import MessageTemplatesView from '@/views/MessageTemplatesView.vue'
import MessageDeliveriesView from '@/views/MessageDeliveriesView.vue'
import NotFoundView from '@/views/NotFoundView.vue'

/**
 * 路由表 —— 静态声明，与菜单配置（config/menu.ts）解耦。
 *
 * 范式说明（相对参考项目的修正）：
 * - 不做「菜单驱动动态注册路由」：菜单只是导航配置，路由是确定的静态表，
 *   避免运行时 addRoute 带来的类型丢失与守卫复杂度；
 * - 未匹配路径落到 catch-all 404，绝不 return to.fullPath 造成重定向死循环；
 * - 登录页走 meta.layout = 'blank'，与后台布局路由天然分离（由 App.vue 分流渲染）。
 */

declare module 'vue-router' {
  interface RouteMeta {
    /** 是否需要登录（默认 true）。 */
    requiresAuth?: boolean
    /** 页面标题（用于 document.title 与标签页）。 */
    title?: string
    /** 布局：'admin'（后台布局，默认）| 'blank'（裸渲染，如登录页）。 */
    layout?: 'admin' | 'blank'
    /** 访问该页所需的权限（后端 RBAC 常量）。 */
    permission?: PermissionKey
  }
}

export const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: LoginView,
    meta: { requiresAuth: false, title: '登录', layout: 'blank' },
  },
  {
    path: '/',
    component: AdminLayout,
    children: [
      { path: '', redirect: '/dashboard' },
      {
        path: 'dashboard',
        name: 'dashboard',
        component: DashboardView,
        meta: { title: '仪表盘' },
      },
      {
        path: 'users',
        name: 'users',
        component: UsersView,
        meta: { title: '用户管理', permission: 'users:read' },
      },
      {
        path: 'dicts',
        name: 'dicts',
        component: DictsView,
        meta: { title: '字典管理', permission: 'dicts:write' },
      },
      {
        path: 'settings',
        redirect: '/settings/basic',
      },
      {
        path: 'settings/basic',
        name: 'settings-basic',
        component: BasicSettingsView,
        meta: { title: '基础设置', permission: 'dicts:write' },
      },
      {
        path: 'settings/seo',
        name: 'settings-seo',
        component: SeoSettingsView,
        meta: { title: 'SEO设置', permission: 'dicts:write' },
      },
      {
        path: 'files',
        name: 'files',
        component: FilesView,
        meta: { title: '文件管理', permission: 'files:read' },
      },
      {
        path: 'messages',
        name: 'messages',
        component: MessagesView,
        meta: { title: '消息中心', permission: 'messages:read' },
      },
      {
        path: 'message-admin',
        redirect: '/messages/send',
      },
      {
        path: 'messages/send',
        name: 'message-send',
        component: SendMessageView,
        meta: { title: '发送消息', permission: 'messages:send' },
      },
      {
        path: 'message-templates',
        name: 'message-templates',
        component: MessageTemplatesView,
        meta: { title: '消息模板', permission: 'messages:manage' },
      },
      {
        path: 'message-deliveries',
        name: 'message-deliveries',
        component: MessageDeliveriesView,
        meta: { title: '投递记录', permission: 'messages:manage' },
      },
      // catch-all：未匹配路由渲染 404（保持在后台布局内）
      {
        path: ':pathMatch(.*)*',
        name: 'not-found',
        component: NotFoundView,
        meta: { title: '页面不存在' },
      },
    ],
  },
]
