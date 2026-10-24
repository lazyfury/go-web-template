import type { PageParams } from './api'
import type { components } from './openapi'

type Schemas = components['schemas']

/**
 * 用户 API 类型由 `docs/openapi.json` 生成（`pnpm gen:api`）。
 * 后端改 DTO 后先 `make openapi`，再 `pnpm gen:api`，不要手写重复接口。
 */
export type User = Schemas['UserDTO']

/** 用户状态，与后端 OpenAPI enum 对齐：1=启用 0=禁用。 */
export const UserStatus = {
  ACTIVE: 1,
  DISABLED: 0,
} as const

export type UserStatusValue = User['status']

/** 创建用户请求体（POST /api/v1/users）。 */
export type CreateUserPayload = Schemas['CreateRequest']

/** 更新用户请求体（PATCH /api/v1/users/:id，未传字段不更新）。 */
export type UpdateUserPayload = Schemas['UpdateRequest']

/** 用户列表响应。 */
export type UserPage = Schemas['ListResponse']

/** 用户列表查询参数。 */
export interface UserListParams extends PageParams {
  keyword?: string
}

/** 用户状态展示元数据（tag 颜色等）。 */
export const UserStatusMeta: Record<UserStatusValue, { label: string; tagType: 'success' | 'info' }> = {
  [UserStatus.ACTIVE]: { label: '启用', tagType: 'success' },
  [UserStatus.DISABLED]: { label: '禁用', tagType: 'info' },
}
