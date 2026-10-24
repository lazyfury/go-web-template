import type { PageParams } from './api'
import type { components } from './openapi'

type Schemas = components['schemas']

/**
 * 字典/配置 API 类型由 `docs/openapi.json` 生成（`pnpm gen:api`）。
 * 后端 DTO 变更后先 `make openapi`，再 `pnpm gen:api`。
 */
export type DictGroup = Schemas['GroupDTO']
export type DictItem = Schemas['ItemDTO']
export type DictGroupPage = Schemas['GroupListResponse']

export type CreateDictGroupPayload = Schemas['CreateGroupRequest']
export type UpdateDictGroupPayload = Schemas['UpdateGroupRequest']
export type CreateDictItemPayload = Schemas['CreateItemRequest']
export type UpdateDictItemPayload = Schemas['UpdateItemRequest']

/** 分组列表查询参数。 */
export interface DictGroupListParams extends PageParams {
  keyword?: string
}

/** 状态值与后端约定一致：1=启用 0=停用。 */
export const DictStatus = {
  ENABLED: 1,
  DISABLED: 0,
} as const

export type DictStatusValue = DictGroup['status']

export const DictStatusMeta: Record<DictStatusValue, { label: string; tagType: 'success' | 'info' }> = {
  [DictStatus.ENABLED]: { label: '启用', tagType: 'success' },
  [DictStatus.DISABLED]: { label: '停用', tagType: 'info' },
}

/** item.value 的解码方式，与后端 value_type 对齐。 */
export const DictValueTypeMeta: Record<DictItem['value_type'], string> = {
  string: '字符串',
  number: '数字',
  boolean: '布尔',
  json: 'JSON',
}
