<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { messageApi } from '@/api/message'
import { usePagination } from '@/composables/usePagination'
import {
  DeliveryStatusMeta,
  MessageChannelMeta,
  type DeliveryStatus,
  type MessageChannel,
  type MessageDelivery,
} from '@/types/message'
import { notifyError } from '@/utils/feedback'

/** 外部渠道投递记录：查看 worker 状态并手动重试。 */
const { page, pageSize, total, loading, reset, onPageChange, onPageSizeChange } = usePagination()
const items = ref<MessageDelivery[]>([])
const statusFilter = ref<DeliveryStatus | ''>('')
const channelFilter = ref<MessageChannel | ''>('')

const load = async () => {
  loading.value = true
  try {
    const result = await messageApi.deliveries({
      page: page.value,
      page_size: pageSize.value,
      status: statusFilter.value === '' ? undefined : statusFilter.value,
      channel: channelFilter.value || undefined,
    })
    items.value = result.items
    total.value = result.total
  } catch (error) {
    notifyError(error, '加载投递记录失败')
  } finally {
    loading.value = false
  }
}

const onFilterChange = async () => {
  reset()
  await load()
}

const retry = async (delivery: MessageDelivery) => {
  try {
    await messageApi.retryDelivery(delivery.id)
    ElMessage.success('已重新入队')
    await load()
  } catch (error) {
    notifyError(error, '重试失败')
  }
}

const statusOptions = (Object.keys(DeliveryStatusMeta).map(Number) as DeliveryStatus[]).map((value) => ({
  value,
  label: DeliveryStatusMeta[value].label,
}))
const channelOptions = (['email', 'sms', 'webhook'] as MessageChannel[]).map((value) => ({
  value,
  label: MessageChannelMeta[value].label,
}))

const formatTime = (value: string) => new Date(value).toLocaleString()
const canRetry = (status: DeliveryStatus) => status === 3 || status === 4

onMounted(load)
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <div class="app-card">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-5 py-4">
        <div>
          <h2 class="text-base font-semibold">投递记录</h2>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">
            邮件/短信/Webhook 的 outbox；worker 按指数退避重试，超过上限变「已终止」。
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <el-select v-model="channelFilter" clearable placeholder="全部渠道" class="w-32" @change="onFilterChange">
            <el-option v-for="option in channelOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
          <el-select v-model="statusFilter" clearable placeholder="全部状态" class="w-32" @change="onFilterChange">
            <el-option v-for="option in statusOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
          <el-button @click="load"><span class="i-ep-refresh-left mr-1" />刷新</el-button>
        </div>
      </div>

      <el-table v-loading="loading" :data="items" class="px-2">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="message_id" label="消息ID" width="90" />
        <el-table-column label="渠道" width="90">
          <template #default="{ row }">{{ MessageChannelMeta[row.channel as 'email' | 'sms' | 'webhook'].label }}</template>
        </el-table-column>
        <el-table-column prop="target" label="目标" min-width="180" show-overflow-tooltip />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="DeliveryStatusMeta[row.status as 0 | 1 | 2 | 3 | 4 | 5].tagType" size="small" effect="light">
              {{ DeliveryStatusMeta[row.status as 0 | 1 | 2 | 3 | 4 | 5].label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="尝试" width="90">
          <template #default="{ row }">{{ row.attempts }} / {{ row.max_attempts }}</template>
        </el-table-column>
        <el-table-column label="下次尝试" width="170">
          <template #default="{ row }">{{ formatTime(row.next_attempt_at) }}</template>
        </el-table-column>
        <el-table-column label="最近错误" min-width="220">
          <template #default="{ row }">
            <el-tooltip v-if="row.last_error" :content="row.last_error" placement="top">
              <span class="block truncate text-xs text-[var(--danger)]">{{ row.last_error }}</span>
            </el-tooltip>
            <span v-else class="text-xs text-[var(--text-muted)]">—</span>
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="170">
          <template #default="{ row }">{{ formatTime(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button v-if="canRetry(row.status as 0 | 1 | 2 | 3 | 4 | 5)" link type="primary" @click="retry(row as MessageDelivery)">
              重试
            </el-button>
            <span v-else class="text-xs text-[var(--text-muted)]">—</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无投递记录" :image-size="72" />
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
  </div>
</template>
