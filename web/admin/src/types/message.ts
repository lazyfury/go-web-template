import type { PageParams } from './api'
import type { components } from './openapi'

type Schemas = components['schemas']

/** 消息/通知 API 类型由 `docs/openapi.json` 生成（`pnpm gen:api`）。 */
export type InboxMessage = Schemas['InboxItemDTO']
export type InboxPage = Schemas['InboxResponse']
export type UnreadCount = Schemas['UnreadCountResponse']

export type SentMessage = Schemas['SentItemDTO']
export type SentPage = Schemas['SentListResponse']
export type SendResult = Schemas['SendResultDTO']

export type MessageTemplate = Schemas['TemplateDTO']
export type TemplatePage = Schemas['TemplateListResponse']
export type CreateTemplatePayload = Schemas['CreateTemplateRequest']
export type UpdateTemplatePayload = Schemas['UpdateTemplateRequest']

export type MessageDelivery = Schemas['DeliveryDTO']
export type DeliveryPage = Schemas['DeliveryListResponse']

export type MessageChannel = 'inbox' | 'email' | 'sms' | 'webhook'
export type MessageType = InboxMessage['type']
export type MessageLevel = InboxMessage['level']
export type DeliveryStatus = MessageDelivery['status']
export type TenantRole = 'admin' | 'member'

/** 发送请求：OpenAPI 的 channels 退化为 string[]，这里收窄回联合类型。 */
export interface SendMessagePayload extends Omit<Schemas['SendRequest'], 'channels' | 'recipients'> {
  channels: MessageChannel[]
  recipients: {
    all?: boolean
    user_ids?: number[]
    roles?: TenantRole[]
  }
}

export interface InboxParams extends PageParams {
  unread_only?: boolean
}

export interface TemplateParams extends PageParams {
  channel?: MessageChannel
  status?: 0 | 1
}

export interface DeliveryParams extends PageParams {
  status?: DeliveryStatus
  channel?: MessageChannel
}

export const MessageChannelMeta: Record<MessageChannel, { label: string; hint: string }> = {
  inbox: { label: '站内信', hint: '写入用户收件箱' },
  email: { label: '邮件', hint: '发送到用户邮箱（需配置 SMTP）' },
  sms: { label: '短信', hint: '发送到用户手机号（需配置 Twilio）' },
  webhook: { label: 'Webhook', hint: 'POST 到服务端白名单地址' },
}

export const MessageTypeMeta: Record<MessageType, { label: string; tagType: 'info' | 'warning' | 'danger' }> = {
  system: { label: '系统', tagType: 'info' },
  announcement: { label: '公告', tagType: 'warning' },
  alert: { label: '告警', tagType: 'danger' },
}

export const MessageLevelMeta: Record<MessageLevel, { label: string; tagType: 'info' | 'success' | 'warning' | 'danger' }> = {
  info: { label: '普通', tagType: 'info' },
  success: { label: '成功', tagType: 'success' },
  warning: { label: '警告', tagType: 'warning' },
  error: { label: '错误', tagType: 'danger' },
}

export const DeliveryStatusMeta: Record<
  DeliveryStatus,
  { label: string; tagType: 'info' | 'success' | 'warning' | 'danger' }
> = {
  0: { label: '待发送', tagType: 'info' },
  1: { label: '发送中', tagType: 'warning' },
  2: { label: '已发送', tagType: 'success' },
  3: { label: '失败', tagType: 'danger' },
  4: { label: '已终止', tagType: 'danger' },
  5: { label: '已取消', tagType: 'info' },
}

export const TemplateStatusMeta: Record<0 | 1, { label: string; tagType: 'success' | 'info' }> = {
  1: { label: '启用', tagType: 'success' },
  0: { label: '停用', tagType: 'info' },
}
