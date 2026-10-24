/** 认证相关类型：全部来自 docs/openapi.json 生成的 schema，不要手写重复接口。 */
import type { components } from './openapi'

type Schemas = components['schemas']

/** 登录请求体（POST /api/v1/auth/login）。 */
export type LoginPayload = Schemas['LoginRequest']

/** 登录响应的用户部分（含角色与权限）。 */
export type AuthUser = Schemas['UserView']

/** 登录/刷新响应：access token 在 body，refresh token 在 HttpOnly cookie。 */
export type LoginResult = Schemas['LoginResponse']

/** 修改密码请求体（PATCH /api/v1/auth/password）。 */
export type ChangePasswordPayload = Schemas['ChangePasswordRequest']

/** 权限标识（后端 authkit.Permission），如 users:read / users:write:self。 */
export type Permission = AuthUser['permissions'][number]

/** 角色。 */
export type Role = AuthUser['role']
