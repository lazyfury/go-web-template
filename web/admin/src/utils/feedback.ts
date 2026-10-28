import { ApiError } from '@/api/http'

/** 统一错误提示：优先取业务错误消息，兜底用 fallback。 */
export const notifyError = (error: unknown, fallback: string) => {
  if (error instanceof ApiError) {
    ElMessage.error(error.message)
    return
  }
  const message = error instanceof Error ? error.message : fallback
  ElMessage.error(message || fallback)
}
