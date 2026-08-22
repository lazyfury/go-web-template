import type { PageParams } from './api'

/** 用户状态，与后端 model.UserStatus 对齐：1=启用 0=禁用。 */
export const UserStatus = {
  ACTIVE: 1,
  DISABLED: 0,
} as const

export type UserStatusValue = (typeof UserStatus)[keyof typeof UserStatus]

/** 用户实体（internal/model.User 的 JSON 形态）。 */
export interface User {
  id: number
  name: string
  email: string
  status: UserStatusValue
  created_at: string
  updated_at: string
}

/** 创建用户请求体（POST /api/v1/users）。 */
export interface CreateUserPayload {
  name: string
  email: string
}

/** 更新用户请求体（PATCH /api/v1/users/:id，指针语义：未传字段不更新）。 */
export interface UpdateUserPayload {
  name?: string
  email?: string
}

/** 用户列表查询参数。 */
export interface UserListParams extends PageParams {
  keyword?: string
}

/** 用户状态展示元数据（tag 颜色等）。 */
export const UserStatusMeta: Record<UserStatusValue, { label: string; tagType: 'success' | 'info' }> = {
  [UserStatus.ACTIVE]: { label: '启用', tagType: 'success' },
  [UserStatus.DISABLED]: { label: '禁用', tagType: 'info' },
}
