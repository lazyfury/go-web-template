<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { messageApi } from '@/api/message'
import { usePagination } from '@/composables/usePagination'
import { useMessageStore } from '@/stores/messages'
import {
  MessageChannelMeta,
  MessageLevelMeta,
  MessageTypeMeta,
  type MessageChannel,
  type MessageLevel,
  type MessageTemplate,
  type MessageType,
  type SentMessage,
  type TenantRole,
} from '@/types/message'
import { notifyError } from '@/utils/feedback'

/** 管理员发送消息 + 最近发送记录。 */
const messagesStore = useMessageStore()
const templates = ref<MessageTemplate[]>([])
const submitting = ref(false)
const sentLoading = ref(false)
const sentItems = ref<SentMessage[]>([])
const { page, pageSize, total, onPageChange, onPageSizeChange } = usePagination(10)

const form = reactive({
  title: '',
  body: '',
  type: 'system' as MessageType,
  level: 'info' as MessageLevel,
  link: '',
  channels: ['inbox'] as MessageChannel[],
  recipientsMode: 'all' as 'all' | 'users' | 'roles',
  userIds: '',
  roles: ['member'] as TenantRole[],
  templateKey: '',
  locale: 'zh-CN',
  dataText: '{}',
})

const channelOptions = (Object.keys(MessageChannelMeta) as MessageChannel[]).map((value) => ({
  value,
  ...MessageChannelMeta[value],
}))
const typeOptions = (Object.keys(MessageTypeMeta) as MessageType[]).map((value) => ({
  value,
  label: MessageTypeMeta[value].label,
}))
const levelOptions = (Object.keys(MessageLevelMeta) as MessageLevel[]).map((value) => ({
  value,
  label: MessageLevelMeta[value].label,
}))

const templateKeys = computed(() => [...new Set(templates.value.map((template) => template.key))])
const selectedTemplates = computed(() => templates.value.filter((template) => template.key === form.templateKey))
const templateChannels = computed(() => selectedTemplates.value.map((template) => template.channel))
const missingTemplateChannels = computed(() =>
  form.templateKey ? form.channels.filter((channel) => !templateChannels.value.includes(channel)) : [],
)

watch(
  () => form.templateKey,
  (key) => {
    if (!key) return
    const template = templates.value.find((item) => item.key === key)
    if (!template || template.variables.length === 0) return
    form.dataText = JSON.stringify(
      Object.fromEntries(template.variables.map((variable) => [variable, ''])),
      null,
      2,
    )
  },
)

const channelDisabled = (channel: MessageChannel) =>
  Boolean(form.templateKey) && !templateChannels.value.includes(channel)

const parseUserIDs = () => {
  const ids = form.userIds
    .split(/[\s,，]+/)
    .map((value) => value.trim())
    .filter(Boolean)
    .map(Number)
  if (ids.length === 0 || ids.some((id) => !Number.isInteger(id) || id <= 0)) {
    throw new Error('请输入有效的用户 ID，用逗号分隔')
  }
  return ids
}

const parseData = () => {
  let value: unknown
  try {
    value = JSON.parse(form.dataText || '{}')
  } catch {
    throw new Error('模板变量必须是合法 JSON')
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('模板变量必须是 JSON 对象')
  }
  return value as Record<string, unknown>
}

const submit = async () => {
  if (form.channels.length === 0) {
    ElMessage.warning('至少选择一个发送渠道')
    return
  }
  if (form.templateKey && missingTemplateChannels.value.length > 0) {
    ElMessage.warning(`模板缺少渠道：${missingTemplateChannels.value.map((c) => MessageChannelMeta[c].label).join('、')}`)
    return
  }
  if (!form.templateKey && (!form.title.trim() || !form.body.trim())) {
    ElMessage.warning('未使用模板时，标题和正文不能为空')
    return
  }

  let recipients: { all?: boolean; user_ids?: number[]; roles?: TenantRole[] }
  try {
    if (form.recipientsMode === 'all') {
      recipients = { all: true }
    } else if (form.recipientsMode === 'users') {
      recipients = { user_ids: parseUserIDs() }
    } else {
      if (form.roles.length === 0) throw new Error('至少选择一个角色')
      recipients = { roles: form.roles }
    }
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : '收件人配置有误')
    return
  }

  let data: Record<string, unknown> | undefined
  try {
    data = form.templateKey ? parseData() : undefined
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : '模板变量有误')
    return
  }

  submitting.value = true
  try {
    const result = await messageApi.send({
      title: form.title.trim() || undefined,
      body: form.body.trim() || undefined,
      type: form.type,
      level: form.level,
      link: form.link.trim() || undefined,
      channels: form.channels,
      recipients,
      template_key: form.templateKey || undefined,
      locale: form.locale.trim() || undefined,
      data,
    })
    ElMessage.success(`发送成功：${result.recipient_count} 位收件人，${result.delivery_count} 个投递任务`)
    await Promise.all([loadSent(), messagesStore.refreshUnread()])
  } catch (error) {
    notifyError(error, '发送失败')
  } finally {
    submitting.value = false
  }
}

const loadTemplates = async () => {
  try {
    const result = await messageApi.templates({ page: 1, page_size: 100, status: 1 })
    templates.value = result.items
  } catch (error) {
    notifyError(error, '加载模板失败')
  }
}

const loadSent = async () => {
  sentLoading.value = true
  try {
    const result = await messageApi.sent({ page: page.value, page_size: pageSize.value })
    sentItems.value = result.items
    total.value = result.total
  } catch (error) {
    notifyError(error, '加载发送记录失败')
  } finally {
    sentLoading.value = false
  }
}

const formatTime = (value: string) => new Date(value).toLocaleString()
const audienceLabel = (audience: SentMessage['audience']) =>
  audience === 'all' ? '全员' : audience === 'role' ? '按角色' : '指定用户'

onMounted(async () => {
  await Promise.all([loadTemplates(), loadSent()])
})
</script>

<template>
  <div class="app-page space-y-4 p-4 md:p-5">
    <div class="app-card overflow-hidden">
      <div class="flex flex-wrap items-start justify-between gap-3 border-b border-line px-5 py-4">
        <div>
          <h2 class="text-base font-semibold">发送消息</h2>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">
            站内信立即写入收件箱；邮件/短信/Webhook 写入投递队列，由后台 worker 发送并重试。
          </p>
        </div>
        <el-button type="primary" :loading="submitting" @click="submit">
          <span class="i-ep-promotion mr-1" />发送
        </el-button>
      </div>

      <el-form label-position="top" class="p-5">
        <div class="grid gap-x-4 md:grid-cols-2">
          <el-form-item label="类型">
            <el-select v-model="form.type" class="w-full">
              <el-option v-for="option in typeOptions" :key="option.value" :label="option.label" :value="option.value" />
            </el-select>
          </el-form-item>
          <el-form-item label="级别">
            <el-select v-model="form.level" class="w-full">
              <el-option v-for="option in levelOptions" :key="option.value" :label="option.label" :value="option.value" />
            </el-select>
          </el-form-item>
        </div>

        <el-form-item label="模板（可选）">
          <el-select v-model="form.templateKey" class="w-full" clearable placeholder="选择模板后，标题/正文可留空">
            <el-option v-for="key in templateKeys" :key="key" :label="key" :value="key" />
          </el-select>
          <div v-if="form.templateKey" class="mt-1 flex flex-wrap items-center gap-1 text-xs text-[var(--text-secondary)]">
            可用渠道：
            <el-tag v-for="channel in templateChannels" :key="channel" size="small" effect="plain">
              {{ MessageChannelMeta[channel].label }}
            </el-tag>
          </div>
        </el-form-item>

        <el-form-item label="渠道">
          <el-checkbox-group v-model="form.channels">
            <el-checkbox
              v-for="option in channelOptions"
              :key="option.value"
              :value="option.value"
              :disabled="channelDisabled(option.value)"
            >
              {{ option.label }}
              <span class="ml-1 text-xs text-[var(--text-muted)]">{{ option.hint }}</span>
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>

        <div class="grid gap-x-4 md:grid-cols-2">
          <el-form-item label="标题">
            <el-input v-model="form.title" maxlength="128" show-word-limit placeholder="模板已提供时可留空" />
          </el-form-item>
          <el-form-item label="跳转链接（可选）">
            <el-input v-model="form.link" maxlength="512" placeholder="/dashboard 或 https://..." />
          </el-form-item>
        </div>

        <el-form-item label="正文">
          <el-input
            v-model="form.body"
            type="textarea"
            :rows="4"
            maxlength="20000"
            show-word-limit
            placeholder="模板已提供时可留空"
          />
        </el-form-item>

        <el-form-item label="收件人">
          <el-radio-group v-model="form.recipientsMode">
            <el-radio value="all">全员</el-radio>
            <el-radio value="users">指定用户</el-radio>
            <el-radio value="roles">按角色</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="form.recipientsMode === 'users'" label="用户 ID">
          <el-input v-model="form.userIds" placeholder="用逗号分隔，如 2,3,4" />
        </el-form-item>
        <el-form-item v-else-if="form.recipientsMode === 'roles'" label="角色">
          <el-select v-model="form.roles" multiple class="w-full">
            <el-option label="管理员" value="admin" />
            <el-option label="成员" value="member" />
          </el-select>
        </el-form-item>

        <div class="grid gap-x-4 md:grid-cols-2">
          <el-form-item label="模板语言">
            <el-input v-model="form.locale" placeholder="zh-CN" />
          </el-form-item>
          <el-form-item label="模板变量（JSON 对象）">
            <el-input v-model="form.dataText" type="textarea" :rows="3" placeholder='{"Name":"Mia"}' />
          </el-form-item>
        </div>

        <el-alert
          v-if="form.channels.includes('sms')"
          type="info"
          :closable="false"
          show-icon
          title="短信只会发送给已设置手机号的用户；没有可用手机号时接口会返回 400。"
        />
        <el-alert
          v-if="form.channels.includes('webhook')"
          type="info"
          :closable="false"
          show-icon
          title="Webhook 目标来自服务端配置的白名单，管理端不能填写任意 URL。"
        />
      </el-form>
    </div>

    <div class="app-card overflow-hidden">
      <div class="flex items-center justify-between border-b border-line px-5 py-4">
        <div>
          <h2 class="text-base font-semibold">最近发送</h2>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">站内信收件人数与已读数。</p>
        </div>
        <el-button @click="loadSent"><span class="i-ep-refresh-left mr-1" />刷新</el-button>
      </div>
      <el-table v-loading="sentLoading" :data="sentItems" class="px-2">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="title" label="标题" min-width="220" />
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="MessageTypeMeta[row.type as 'system' | 'announcement' | 'alert'].tagType" size="small" effect="light">
              {{ MessageTypeMeta[row.type as 'system' | 'announcement' | 'alert'].label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="收件人" width="110">
          <template #default="{ row }">{{ audienceLabel(row.audience) }}</template>
        </el-table-column>
        <el-table-column label="收件数" width="90">
          <template #default="{ row }">{{ row.recipient_count }}</template>
        </el-table-column>
        <el-table-column label="已读" width="80">
          <template #default="{ row }">{{ row.read_count }}</template>
        </el-table-column>
        <el-table-column label="发送时间" width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无发送记录" :image-size="72" />
        </template>
      </el-table>
      <div class="flex justify-end px-5 py-4">
        <el-pagination
          :current-page="page"
          :page-size="pageSize"
          :page-sizes="[10, 20, 50]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @update:current-page="onPageChange"
          @update:page-size="onPageSizeChange"
          @current-change="loadSent"
          @size-change="loadSent"
        />
      </div>
    </div>
  </div>
</template>
