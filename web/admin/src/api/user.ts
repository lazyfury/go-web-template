import { request } from './http'
import type {
  CreateUserPayload,
  UpdateUserPayload,
  User,
  UserListParams,
} from '@/types/user'
import type { PageResult } from '@/types/api'

/**
 * 用户领域 API。直接对接 go-web-template 后端 /api/v1/users。
 */
export const userApi = {
  /** GET /api/v1/users?page&page_size&keyword */
  list(params: UserListParams): Promise<PageResult<User>> {
    return request.get<PageResult<User>>('/users', { params })
  },

  /** POST /api/v1/users */
  create(payload: CreateUserPayload): Promise<User> {
    return request.post<User>('/users', payload)
  },

  /** PATCH /api/v1/users/:id（指针语义） */
  update(id: number, payload: UpdateUserPayload): Promise<User> {
    return request.patch<User>(`/users/${id}`, payload)
  },

  /** DELETE /api/v1/users/:id（软删除，返回 204） */
  remove(id: number): Promise<void> {
    return request.delete<void>(`/users/${id}`)
  },
}
