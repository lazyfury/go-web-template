import { request } from './http'
import type {
  CreateTemplatePayload,
  DeliveryPage,
  DeliveryParams,
  InboxPage,
  InboxParams,
  MessageDelivery,
  MessageTemplate,
  SendMessagePayload,
  SendResult,
  SentPage,
  TemplatePage,
  TemplateParams,
  UnreadCount,
  UpdateTemplatePayload,
} from '@/types/message'

/**
 * 消息/通知 API。
 *
 * 用户侧：/messages/inbox、/messages/unread-count
 * 管理侧：/messages、/messages/sent、/message-templates、/message-deliveries
 */
export const messageApi = {
  inbox(params: InboxParams): Promise<InboxPage> {
    return request.get<InboxPage>('/messages/inbox', { params })
  },

  unreadCount(): Promise<UnreadCount> {
    return request.get<UnreadCount>('/messages/unread-count')
  },

  markRead(id: number): Promise<void> {
    return request.patch<void>(`/messages/inbox/${id}/read`)
  },

  markAllRead(): Promise<void> {
    return request.post<void>('/messages/read-all')
  },

  removeInbox(id: number): Promise<void> {
    return request.delete<void>(`/messages/inbox/${id}`)
  },

  send(payload: SendMessagePayload): Promise<SendResult> {
    return request.post<SendResult>('/messages', payload)
  },

  sent(params: { page?: number; page_size?: number }): Promise<SentPage> {
    return request.get<SentPage>('/messages/sent', { params })
  },

  templates(params: TemplateParams): Promise<TemplatePage> {
    return request.get<TemplatePage>('/message-templates', { params })
  },

  createTemplate(payload: CreateTemplatePayload): Promise<MessageTemplate> {
    return request.post<MessageTemplate>('/message-templates', payload)
  },

  updateTemplate(id: number, payload: UpdateTemplatePayload): Promise<MessageTemplate> {
    return request.patch<MessageTemplate>(`/message-templates/${id}`, payload)
  },

  removeTemplate(id: number): Promise<void> {
    return request.delete<void>(`/message-templates/${id}`)
  },

  deliveries(params: DeliveryParams): Promise<DeliveryPage> {
    return request.get<DeliveryPage>('/message-deliveries', { params })
  },

  retryDelivery(id: number): Promise<void> {
    return request.post<void>(`/message-deliveries/${id}/retry`)
  },
}

export type { MessageDelivery }
