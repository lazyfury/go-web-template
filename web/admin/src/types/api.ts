/**
 * API 统一契约类型。
 *
 * 后端（internal/response）返回统一信封 `{ code, message, data? }`：
 * - code === 0 表示成功
 * - 非 0 为业务错误码（见 internal/errors）：40000/40100/40300/40400/40900/42900/50000/50001
 *
 * 前端约定：`http` 层在响应拦截器统一剥离信封，业务层拿到的是已解包的 `T`，
 * 因此业务代码里不再出现 `ApiResponse<T> | T` 双形态判断。
 */

/** 后端统一响应信封（仅 http 层内部使用）。 */
export interface ApiEnvelope<T = unknown> {
  code: number
  message: string
  data?: T
}

/** 分页接口统一返回结构（GET /api/v1/users 等）。 */
export interface PageResult<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

/** 分页请求参数。 */
export interface PageParams {
  page?: number
  page_size?: number
}

/** 业务错误码（与 internal/errors 对齐）。 */
export const BusinessCode = {
  OK: 0,
  INVALID_ARGUMENT: 40000,
  UNAUTHORIZED: 40100,
  FORBIDDEN: 40300,
  NOT_FOUND: 40400,
  CONFLICT: 40900,
  TOO_MANY_REQUESTS: 42900,
  INTERNAL: 50000,
  DATABASE: 50001,
} as const

export type BusinessCodeValue = (typeof BusinessCode)[keyof typeof BusinessCode]
