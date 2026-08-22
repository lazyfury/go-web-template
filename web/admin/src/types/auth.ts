/** 认证相关类型。 */

/** 登录用户信息（真实接口接入后按后端返回扩展）。 */
export interface AuthUser {
  id: number
  username: string
  email: string
  role: string
}

/** 登录请求体。 */
export interface LoginPayload {
  username: string
  password: string
}

/** 登录响应。 */
export interface LoginResult {
  token: string
  user: AuthUser
}
