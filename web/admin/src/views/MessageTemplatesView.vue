<script setup lang="ts">
import type { FormInstance, FormRules } from 'element-plus'
import { onMounted, reactive, ref } from 'vue'
import { messageApi } from '@/api/message'
import { usePagination } from '@/composables/usePagination'
import {
  MessageChannelMeta,
  TemplateStatusMeta,
  type MessageChannel,
  type MessageTemplate,
} from '@/types/message'
import { notifyError } from '@/utils/feedback'

/** 消息模板管理：按 key + channel + locale 维护。 */
const { page, pageSize, total, loading, reset, onPageChange, onPageSizeChange } = usePagination()
const items = ref<MessageTemplate[]>([])
const channelFilter = ref<MessageChannel | ''>('')
const statusFilter = ref<0 | 1 | ''>('')

const dialogVisible = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const editingId = ref<number | null>(null)
const submitting = ref(false)
const formRef = ref<FormInstance>()
const form = reactive({
  key: '',
  channel: 'inbox' as MessageChannel,
  locale: 'zh-CN',
  subject: '',
  body: '',
  variablesText: '',
  status: 1 as 0 | 1,
})

const rules: FormRules = {
  key: [
    { required: true, message: '请输入模板 key', trigger: 'blur' },
    {
      pattern: /^[a-z][a-z0-9_.-]{0,63}$/,
      message: '小写字母开头，可含数字、下划线、点、连字符',
      trigger: 'blur',
    },
  ],
  body: [{ required: true, message: '请输入模板正文', trigger: 'blur' }],
}

const load = async () => {
  loading.value = true
  try {
    const result = await messageApi.templates({
      page: page.value,
      page_size: pageSize.value,
      channel: channelFilter.value || undefined,
      status: statusFilter.value === '' ? undefined : statusFilter.value,
    })
    items.value = result.items
    total.value = result.total
  } catch (error) {
    notifyError(error, '加载模板失败')
  } finally {
    loading.value = false
  }
}

const onFilterChange = async () => {
  reset()
  await load()
}

const openCreate = () => {
  dialogMode.value = 'create'
  editingId.value = null
  form.key = ''
  form.channel = 'inbox'
  form.locale = 'zh-CN'
  form.subject = ''
  form.body = ''
  form.variablesText = ''
  form.status = 1
  dialogVisible.value = true
}

const openEdit = (template: MessageTemplate) => {
  dialogMode.value = 'edit'
  editingId.value = template.id
  form.key = template.key
  form.channel = template.channel
  form.locale = template.locale
  form.subject = template.subject
  form.body = template.body
  form.variablesText = template.variables.join(', ')
  form.status = template.status
  dialogVisible.value = true
}

const parseVariables = () =>
  form.variablesText
    .split(/[\s,，]+/)
    .map((value) => value.trim())
    .filter(Boolean)

const submit = async () => {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    if (dialogMode.value === 'create') {
      await messageApi.createTemplate({
        key: form.key.trim(),
        channel: form.channel,
        locale: form.locale.trim() || 'zh-CN',
        subject: form.subject,
        body: form.body,
        variables: parseVariables(),
        status: form.status,
      })
      ElMessage.success('创建成功')
    } else if (editingId.value !== null) {
      await messageApi.updateTemplate(editingId.value, {
        subject: form.subject,
        body: form.body,
        variables: parseVariables(),
        status: form.status,
      })
      ElMessage.success('保存成功')
    }
    dialogVisible.value = false
    await load()
  } catch (error) {
    notifyError(error, dialogMode.value === 'create' ? '创建模板失败' : '保存模板失败')
  } finally {
    submitting.value = false
  }
}

const remove = async (template: MessageTemplate) => {
  try {
    await ElMessageBox.confirm(`确定删除模板「${template.key} / ${template.channel}」吗？`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      confirmButtonClass: 'el-button--danger',
    })
  } catch {
    return
  }
  try {
    await messageApi.removeTemplate(template.id)
    ElMessage.success('删除成功')
    await load()
  } catch (error) {
    notifyError(error, '删除模板失败')
  }
}

const channelOptions = (Object.keys(MessageChannelMeta) as MessageChannel[]).map((value) => ({
  value,
  label: MessageChannelMeta[value].label,
}))

const formatTime = (value: string) => new Date(value).toLocaleString()

onMounted(load)
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <div class="app-card">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-5 py-4">
        <div>
          <h2 class="text-base font-semibold">消息模板</h2>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">key + 渠道 + 语言唯一；发送时按渠道取模板并渲染快照。</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <el-select v-model="channelFilter" clearable placeholder="全部渠道" class="w-32" @change="onFilterChange">
            <el-option v-for="option in channelOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
          <el-select v-model="statusFilter" clearable placeholder="全部状态" class="w-28" @change="onFilterChange">
            <el-option label="启用" :value="1" />
            <el-option label="停用" :value="0" />
          </el-select>
          <el-button type="primary" @click="openCreate">
            <span class="i-ep-plus mr-1" />新建模板
          </el-button>
        </div>
      </div>

      <el-table v-loading="loading" :data="items" class="px-2">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="key" label="Key" min-width="170">
          <template #default="{ row }"><span class="font-mono">{{ row.key }}</span></template>
        </el-table-column>
        <el-table-column label="渠道" width="100">
          <template #default="{ row }">{{ MessageChannelMeta[row.channel as 'inbox' | 'email' | 'sms' | 'webhook'].label }}</template>
        </el-table-column>
        <el-table-column prop="locale" label="语言" width="90" />
        <el-table-column prop="subject" label="标题模板" min-width="180" show-overflow-tooltip />
        <el-table-column label="变量" min-width="140">
          <template #default="{ row }">
            <el-tag v-for="variable in row.variables" :key="variable" size="small" effect="plain" class="mr-1">
              {{ variable }}
            </el-tag>
            <span v-if="row.variables.length === 0" class="text-xs text-[var(--text-muted)]">无</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="TemplateStatusMeta[row.status as 0 | 1].tagType" size="small" effect="light">
              {{ TemplateStatusMeta[row.status as 0 | 1].label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="170">
          <template #default="{ row }">{{ formatTime(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row as MessageTemplate)">编辑</el-button>
            <el-button link type="danger" @click="remove(row as MessageTemplate)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无模板" :image-size="72" />
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
          @current-change="load"
          @size-change="load"
        />
      </div>
    </div>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogMode === 'create' ? '新建消息模板' : '编辑消息模板'"
      width="640px"
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="grid gap-x-4 md:grid-cols-3">
          <el-form-item label="Key" prop="key">
            <el-input v-model="form.key" :disabled="dialogMode === 'edit'" placeholder="如 user.welcome" />
          </el-form-item>
          <el-form-item label="渠道">
            <el-select v-model="form.channel" class="w-full" :disabled="dialogMode === 'edit'">
              <el-option v-for="option in channelOptions" :key="option.value" :label="option.label" :value="option.value" />
            </el-select>
          </el-form-item>
          <el-form-item label="语言">
            <el-input v-model="form.locale" :disabled="dialogMode === 'edit'" placeholder="zh-CN" />
          </el-form-item>
        </div>
        <el-form-item label="标题模板">
          <el-input v-model="form.subject" maxlength="255" placeholder="支持 {{.Name}} 变量；短信/Webhook 可留空" />
        </el-form-item>
        <el-form-item label="正文模板" prop="body">
          <el-input v-model="form.body" type="textarea" :rows="6" placeholder="Hello {{.Name}}" />
        </el-form-item>
        <el-form-item label="允许变量">
          <el-input v-model="form.variablesText" placeholder="用逗号分隔，如 Name, Email" />
          <p class="mt-1 text-xs text-[var(--text-muted)]">发送时 data 只能包含这里声明的变量；缺失变量会渲染失败。</p>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="form.status" class="w-full">
            <el-option label="启用" :value="1" />
            <el-option label="停用" :value="0" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
