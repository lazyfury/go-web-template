import { request } from './http'
import type {
  CreateDictGroupPayload,
  CreateDictItemPayload,
  DictGroup,
  DictGroupListParams,
  DictGroupPage,
  DictItem,
  UpdateDictGroupPayload,
  UpdateDictItemPayload,
} from '@/types/dict'

/**
 * 字典/配置领域 API。
 *
 * - 管理端：/dict-groups、/dict-items（需要 dicts:write）
 * - 消费端：/dicts/:group（需要 dicts:read，返回启用项数组）
 */
export const dictApi = {
  /** GET /api/v1/dict-groups?page&page_size&keyword */
  listGroups(params: DictGroupListParams): Promise<DictGroupPage> {
    return request.get<DictGroupPage>('/dict-groups', { params })
  },

  /** GET /api/v1/dict-groups/:id */
  getGroup(id: number): Promise<DictGroup> {
    return request.get<DictGroup>(`/dict-groups/${id}`)
  },

  /** POST /api/v1/dict-groups */
  createGroup(payload: CreateDictGroupPayload): Promise<DictGroup> {
    return request.post<DictGroup>('/dict-groups', payload)
  },

  /** PATCH /api/v1/dict-groups/:id（key 不可改） */
  updateGroup(id: number, payload: UpdateDictGroupPayload): Promise<DictGroup> {
    return request.patch<DictGroup>(`/dict-groups/${id}`, payload)
  },

  /** DELETE /api/v1/dict-groups/:id（软删除并级联软删 items） */
  removeGroup(id: number): Promise<void> {
    return request.delete<void>(`/dict-groups/${id}`)
  },

  /** GET /api/v1/dict-groups/:id/items?include_disabled=true */
  listItems(groupId: number, includeDisabled = true): Promise<DictItem[]> {
    return request.get<DictItem[]>(`/dict-groups/${groupId}/items`, {
      params: { include_disabled: includeDisabled },
    })
  },

  /** POST /api/v1/dict-groups/:id/items */
  createItem(groupId: number, payload: CreateDictItemPayload): Promise<DictItem> {
    return request.post<DictItem>(`/dict-groups/${groupId}/items`, payload)
  },

  /** PATCH /api/v1/dict-items/:id */
  updateItem(id: number, payload: UpdateDictItemPayload): Promise<DictItem> {
    return request.patch<DictItem>(`/dict-items/${id}`, payload)
  },

  /** DELETE /api/v1/dict-items/:id（软删除） */
  removeItem(id: number): Promise<void> {
    return request.delete<void>(`/dict-items/${id}`)
  },

  /** GET /api/v1/dicts/:group（消费端：仅启用项，按 sort 排序） */
  values(group: string): Promise<DictItem[]> {
    return request.get<DictItem[]>(`/dicts/${encodeURIComponent(group)}`)
  },
}
