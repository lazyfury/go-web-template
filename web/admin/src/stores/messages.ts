import { defineStore } from 'pinia'
import { ref } from 'vue'
import { messageApi } from '@/api/message'

/**
 * 未读消息状态：顶栏铃铛与消息中心共用。
 * 轮询失败不打扰用户（登录过期由 http 层统一处理）。
 */
export const useMessageStore = defineStore('messages', () => {
  const unreadCount = ref(0)
  let timer: ReturnType<typeof setInterval> | null = null

  const refreshUnread = async () => {
    try {
      const result = await messageApi.unreadCount()
      unreadCount.value = result.count
    } catch {
      // 轮询失败保持旧值；401 由 http 拦截器处理。
    }
  }

  const startPolling = (intervalMs = 30_000) => {
    if (timer !== null) return
    void refreshUnread()
    timer = setInterval(() => {
      void refreshUnread()
    }, intervalMs)
  }

  const stopPolling = () => {
    if (timer !== null) {
      clearInterval(timer)
      timer = null
    }
  }

  const setUnread = (value: number) => {
    unreadCount.value = Math.max(0, value)
  }

  const decrement = () => {
    unreadCount.value = Math.max(0, unreadCount.value - 1)
  }

  return { unreadCount, refreshUnread, startPolling, stopPolling, setUnread, decrement }
})
