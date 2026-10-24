import { createRouter, createWebHistory } from 'vue-router'
import { routes } from './routes'
import { useAuthStore } from '@/stores/auth'
import { setAuthExpiredHandler } from '@/api/http'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

/**
 * 登录态守卫：
 * 1. 先 bootstrap（用 HttpOnly refresh cookie 换 access token），刷新页面后不会掉线；
 * 2. 未登录访问受保护页 → /login?redirect=；已登录访问登录页 → /dashboard；
 * 3. 路由声明了 meta.permission 而当前角色不具备 → 回仪表盘（真正的拦截在后端）。
 */
router.beforeEach(async (to) => {
  const auth = useAuthStore()
  await auth.bootstrap()

  const requiresAuth = to.meta.requiresAuth !== false

  if (to.name === 'login' && auth.isAuthenticated) {
    return { path: '/dashboard' }
  }

  if (requiresAuth && !auth.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }

  if (to.meta.permission && !auth.can(to.meta.permission)) {
    return { path: '/dashboard' }
  }

  return true
})

router.afterEach((to) => {
  const title = to.meta.title
  document.title = title ? `${title} · Admin Console` : 'Admin Console'
})

/** 登录过期：清空本地登录态并在 SPA 内跳转登录页（覆盖 http 层默认整页跳转）。 */
setAuthExpiredHandler(() => {
  useAuthStore().clearAuth()
  const current = router.currentRoute.value.fullPath
  router.replace({ path: '/login', query: { redirect: current } })
})

export default router
