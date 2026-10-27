import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { authApi } from '@/api/auth'
import { notifyError } from '@/utils/feedback'
import { safeRedirectTarget } from '@/utils/redirect'

/**
 * 退出登录：**先等后端吊销 + 本地清态完成，再导航到登录页**。
 *
 * 顺序很重要。旧实现是 fire-and-forget（`authApi.logout()` 不 await 就
 * `router.replace('/login')`）：导航发生时 store 里的 access token 还在，
 * 登录页守卫看到 `isAuthenticated === true` 又把它弹回 `/dashboard`，
 * 于是「退出登录」看起来毫无反应，直到下一次请求 401 才被动重定向。
 *
 * 约定：
 * - 无论后端是否可达，本地登录态都必须清掉（authApi.logout 的 finally 负责）；
 * - 退出前的位置放进 `redirect`，重新登录后回到原页面；
 * - 并发点击只执行一次。
 */
export const useLogout = () => {
  const router = useRouter()
  const route = useRoute()
  const loggingOut = ref(false)

  const logout = async () => {
    if (loggingOut.value) return
    loggingOut.value = true
    const redirect = safeRedirectTarget(route.fullPath)
    try {
      await authApi.logout()
      ElMessage.success('已退出登录')
    } catch (error) {
      // 后端可能已经不可达：本地状态已清，仍要离开当前页，
      // 不给用户留一个「看似已登录」的半死状态。
      notifyError(error, '已退出本地登录，但服务端会话可能仍有效')
    } finally {
      loggingOut.value = false
    }
    await router.replace({ path: '/login', query: { redirect } })
  }

  return { logout, loggingOut }
}
