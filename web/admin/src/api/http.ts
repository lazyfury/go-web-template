import axios, { type AxiosRequestConfig, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { BusinessCode, type ApiEnvelope } from '@/types/api'
import type { LoginResult } from '@/types/auth'

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
/* 会话刷新：401 时单飞（single-flight）调用 /auth/refresh，成功后重放一次   */
/* ------------------------------------------------------------------ */

let sessionUpdater: ((result: LoginResult) => void) | null = null

/** 注册「刷新成功」回调（auth store 用它写入内存中的 access token）。 */
export const setSessionUpdater = (updater: (result: LoginResult) => void) => {
  sessionUpdater = updater
}

/**
 * 独立的 axios 实例：刷新请求不能走 client 的拦截器，
 * 否则 refresh 自己 401 时会递归触发刷新。
 */
const refreshClient = axios.create({ baseURL: '/api/v1', withCredentials: true, timeout: 15000 })

let refreshInFlight: Promise<boolean> | null = null

/**
 * 用 HttpOnly refresh cookie 换新 access token。
 * 并发调用共享同一个请求（single-flight），避免轮换竞态把会话挤掉。
 */
export const refreshSession = (): Promise<boolean> => {
  if (!sessionUpdater) return Promise.resolve(false)
  if (!refreshInFlight) {
    refreshInFlight = refreshClient
      .post<ApiEnvelope<LoginResult>>('/auth/refresh')
      .then((response) => {
        const payload = response.data
        if (!isEnvelope(payload) || payload.code !== BusinessCode.OK || !payload.data) {
          return false
        }
        sessionUpdater?.(payload.data as LoginResult)
        return true
      })
      .catch(() => false)
      .finally(() => {
        refreshInFlight = null
      })
  }
  return refreshInFlight
}

/* ------------------------------------------------------------------ */
/* 401 语义：哪些端点上的 401 不是「会话过期」                            */
/* ------------------------------------------------------------------ */

/**
 * 这些端点的 401 是**业务结果**，不是「登录态失效」：
 * - /auth/login：邮箱或密码错；
 * - /auth/refresh：refresh cookie 无效（bootstrap/单飞刷新自己处理）；
 * - /auth/logout：会话本就已经失效，清本地态即可；
 * - /auth/password：**当前密码错**（后端按 401 返回，避免泄露账号状态）。
 *
 * 对它们既不能刷新（refresh 自己 401 会递归）、也不能清会话/跳登录：
 * 否则「改密时输错当前密码」会被当成掉线直接踢回登录页。
 */
const SESSIONLESS_401_PATHS = new Set(['/auth/login', '/auth/refresh', '/auth/logout', '/auth/password'])

/** shouldRefreshSession 判断一次 401 是否值得走「刷新 + 重放」。 */
export const shouldRefreshSession = (url: string | undefined, retried: boolean): boolean => {
  if (retried) return false
  if (!url) return true
  // 精确匹配到路径本身（去掉 query），避免 /auth/password-reset 之类被误判。
  const path = url.split('?')[0]
  return !SESSIONLESS_401_PATHS.has(path)
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

type RetriableConfig = InternalAxiosRequestConfig & { _retried?: boolean }

const client = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
  // refresh token 走 HttpOnly cookie：同源请求必须带上凭据。
  withCredentials: true,
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
  async (error: unknown) => {
    if (!axios.isAxiosError(error)) {
      return Promise.reject(error)
    }

    const status = error.response?.status ?? 0
    const payload = error.response?.data
    const config = error.config as RetriableConfig | undefined

    // 登录态失效：业务码 40100 或 HTTP 401
    const code = isEnvelope(payload) ? payload.code : status
    const unauthorized = status === 401 || code === BusinessCode.UNAUTHORIZED
    const message = extractErrorMessage(payload, error.message || '请求失败')
    if (!unauthorized || !config) {
      return Promise.reject(new ApiError(message, code, status))
    }

    // 登录/刷新/登出/改密的 401 是业务结果：交给调用方展示，不刷新也不跳转。
    if (!shouldRefreshSession(config.url, Boolean(config._retried))) {
      return Promise.reject(new ApiError(message, code, status))
    }

    // 401 且尚未重试：先用 refresh cookie 换新 token，成功则重放原请求一次。
    config._retried = true
    if (await refreshSession()) {
      return client.request(config)
    }

    handleAuthExpired()
    return Promise.reject(new ApiError('登录已过期，请重新登录', BusinessCode.UNAUTHORIZED, status))
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
  put<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
    return client.put<ApiEnvelope<T>>(url, data, config).then((r) => unwrap<T>(r))
  },
  patch<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
    return client.patch<ApiEnvelope<T>>(url, data, config).then((r) => unwrap<T>(r))
  },
  delete<T = void>(url: string, config?: AxiosRequestConfig): Promise<T> {
    return client.delete<ApiEnvelope<T>>(url, config).then((r) => unwrap<T>(r))
  },
}
