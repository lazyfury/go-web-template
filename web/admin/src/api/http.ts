import axios, { type AxiosRequestConfig, type AxiosResponse } from 'axios'
import { BusinessCode, type ApiEnvelope } from '@/types/api'

/**
 * 业务错误（统一信封解包后抛出）。
 * 携带业务码 code 与 HTTP 状态 status，UI 层据此展示/跳转。
 */
export class ApiError extends Error {
  readonly code: number
  readonly status: number

  constructor(message: string, code: number, status: number) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }
}

const isEnvelope = (payload: unknown): payload is ApiEnvelope =>
  typeof payload === 'object' && payload !== null && 'code' in payload && 'message' in payload

/** 从任意响应体提取用户可读的错误消息（信封优先）。 */
const extractErrorMessage = (payload: unknown, fallback: string): string => {
  if (isEnvelope(payload) && typeof payload.message === 'string' && payload.message) {
    return payload.message
  }
  if (typeof payload === 'object' && payload !== null) {
    const message = (payload as { message?: unknown }).message
    if (typeof message === 'string' && message) {
      return message
    }
  }
  return fallback
}

/* ------------------------------------------------------------------ */
/* token 注入：通过 setAuthTokenProvider 注册，避免 http ↔ store 循环依赖 */
/* ------------------------------------------------------------------ */

let tokenProvider: () => string | null = () => null

/** 注册实时 token 读取函数（由 auth store 初始化时调用）。 */
export const setAuthTokenProvider = (provider: () => string | null) => {
  tokenProvider = provider
}

/* ------------------------------------------------------------------ */
/* 登录过期处理：默认整页跳转；router 模块可覆盖为 SPA 内跳转            */
/* ------------------------------------------------------------------ */

let authExpiredHandler: (() => void) | null = null

export const setAuthExpiredHandler = (handler: () => void) => {
  authExpiredHandler = handler
}

const handleAuthExpired = () => {
  if (authExpiredHandler) {
    authExpiredHandler()
    return
  }
  const current = window.location.pathname + window.location.search
  window.location.href = `/login?redirect=${encodeURIComponent(current)}`
}

/* ------------------------------------------------------------------ */
/* axios 实例                                                          */
/* ------------------------------------------------------------------ */

const client = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

client.interceptors.request.use((config) => {
  const token = tokenProvider()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

client.interceptors.response.use(
  (response: AxiosResponse<ApiEnvelope | unknown>) => {
    const payload = response.data

    // 204 NoContent：无信封，直接返回
    if (response.status === 204 || payload == null) {
      return response
    }

    // 统一信封：业务码非 0 视为失败（即便 HTTP 200，例如 42900）
    if (isEnvelope(payload)) {
      if (payload.code !== BusinessCode.OK) {
        throw new ApiError(payload.message, payload.code, response.status)
      }
      return response
    }

    return response
  },
  (error: unknown) => {
    if (!axios.isAxiosError(error)) {
      return Promise.reject(error)
    }

    const status = error.response?.status ?? 0
    const payload = error.response?.data

    // 登录态失效：业务码 40100 或 HTTP 401
    const code = isEnvelope(payload) ? payload.code : status
    if (status === 401 || code === BusinessCode.UNAUTHORIZED) {
      handleAuthExpired()
      return Promise.reject(new ApiError('登录已过期，请重新登录', BusinessCode.UNAUTHORIZED, status))
    }

    const message = extractErrorMessage(payload, error.message || '请求失败')
    return Promise.reject(new ApiError(message, code, status))
  },
)

/* ------------------------------------------------------------------ */
/* request 封装：响应拦截器已解包统一信封，业务层直接拿到 data 的类型     */
/* ------------------------------------------------------------------ */

const unwrap = <T>(response: AxiosResponse): T => {
  const payload = response.data as ApiEnvelope<T> | T

  // 信封结构（成功）：返回 data；裸数据：原样返回
  if (isEnvelope(payload)) {
    return payload.data as T
  }
  return payload as T
}

export const request = {
  get<T>(url: string, config?: AxiosRequestConfig): Promise<T> {
    return client.get<ApiEnvelope<T>>(url, config).then((r) => unwrap<T>(r))
  },
  post<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
    return client.post<ApiEnvelope<T>>(url, data, config).then((r) => unwrap<T>(r))
  },
  patch<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
    return client.patch<ApiEnvelope<T>>(url, data, config).then((r) => unwrap<T>(r))
  },
  delete<T = void>(url: string, config?: AxiosRequestConfig): Promise<T> {
    return client.delete<ApiEnvelope<T>>(url, config).then((r) => unwrap<T>(r))
  },
}
