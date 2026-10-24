<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { messageApi } from '@/api/message'
import { usePagination } from '@/composables/usePagination'
import { useMessageStore } from '@/stores/messages'
import {
  MessageLevelMeta,
  MessageTypeMeta,
  type InboxMessage,
} from '@/types/message'
import { notifyError } from '@/utils/feedback'

/** 消息中心：当前登录用户的站内信收件箱。 */
const router = useRouter()
const messagesStore = useMessageStore()
const { page, pageSize, total, loading, reset, onPageChange, onPageSizeChange } = usePagination()
const unreadOnly = ref(false)
const items = ref<InboxMessage[]>([])
const dialogVisible = ref(false)
const selected = ref<InboxMessage | null>(null)

const load = async () => {
  loading.value = true
  try {
    const result = await messageApi.inbox({
      page: page.value,
      page_size: pageSize.value,
      unread_only: unreadOnly.value || undefined,
    })
    items.value = result.items
    total.value = result.total
  } catch (error) {
    notifyError(error, '加载消息失败')
  } finally {
    loading.value = false
  }
}

const onFilterChange = async () => {
  reset()
  await load()
}

const openMessage = async (item: InboxMessage) => {
  selected.value = item
  dialogVisible.value = true
  if (!item.read_at) {
    try {
      await messageApi.markRead(item.id)
      item.read_at = new Date().toISOString()
      messagesStore.decrement()
      if (unreadOnly.value) {
        await load()
      }
    } catch (error) {
      notifyError(error, '标记已读失败')
    }
  }
}

const markAllRead = async () => {
  try {
    await messageApi.markAllRead()
    messagesStore.setUnread(0)
    ElMessage.success('已全部标记为已读')
    await load()
  } catch (error) {
    notifyError(error, '操作失败')
  }
}

const removeMessage = async (item: InboxMessage) => {
  try {
    await ElMessageBox.confirm(`确定删除消息「${item.title}」吗？`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      confirmButtonClass: 'el-button--danger',
    })
  } catch {
    return
  }
  try {
    await messageApi.removeInbox(item.id)
    if (!item.read_at) messagesStore.decrement()
    ElMessage.success('删除成功')
    if (selected.value?.id === item.id) dialogVisible.value = false
    await load()
  } catch (error) {
    notifyError(error, '删除失败')
  }
}

const openLink = () => {
  const link = selected.value?.link
  if (!link) return
  dialogVisible.value = false
  if (link.startsWith('http://') || link.startsWith('https://')) {
    window.open(link, '_blank', 'noopener')
    return
  }
  router.push(link)
}

const formatTime = (value: string) => new Date(value).toLocaleString()

onMounted(async () => {
  await load()
  await messagesStore.refreshUnread()
})
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <div class="app-card">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-5 py-4">
        <div>
          <h2 class="text-base font-semibold">消息中心</h2>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">站内信与系统通知，未读消息会同步到顶栏铃铛。</p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex items-center gap-2 text-sm text-[var(--text-secondary)]">
            <el-switch v-model="unreadOnly" @change="onFilterChange" />
            只看未读
          </div>
          <el-button :disabled="messagesStore.unreadCount === 0" @click="markAllRead">
            <span class="i-ep-check mr-1" />全部已读
          </el-button>
          <el-button @click="load">
            <span class="i-ep-refresh-left mr-1" />刷新
          </el-button>
        </div>
      </div>

      <el-table v-loading="loading" :data="items" class="px-2">
        <el-table-column width="42">
          <template #default="{ row }">
            <span
              class="inline-block h-2 w-2 rounded-full"
              :class="row.read_at ? 'bg-[var(--line-strong)]' : 'bg-[var(--brand-500)]'"
            />
          </template>
        </el-table-column>
        <el-table-column label="消息" min-width="260">
          <template #default="{ row }">
            <button type="button" class="block max-w-full cursor-pointer text-left" @click="openMessage(row)">
              <span :class="row.read_at ? 'text-[var(--text-secondary)]' : 'font-semibold text-[var(--text-primary)]'">
                {{ row.title }}
              </span>
              <span class="mt-0.5 block truncate text-xs text-[var(--text-muted)]">{{ row.body }}</span>
            </button>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="MessageTypeMeta[row.type as 'system' | 'announcement' | 'alert'].tagType" size="small" effect="light">
              {{ MessageTypeMeta[row.type as 'system' | 'announcement' | 'alert'].label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="级别" width="90">
          <template #default="{ row }">
            <el-tag :type="MessageLevelMeta[row.level as 'info' | 'success' | 'warning' | 'error'].tagType" size="small" effect="light">
              {{ MessageLevelMeta[row.level as 'info' | 'success' | 'warning' | 'error'].label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openMessage(row)">查看</el-button>
            <el-button link type="danger" @click="removeMessage(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无消息" :image-size="80" />
        </template>
      </el-table>

      <div class="flex justify-end px-5 py-4">
        <el-pagination
          :current-page="page"
          :page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @update:current-page="onPageChange"
          @update:page-size="onPageSizeChange"
          @current-change="load"
          @size-change="load"
        />
      </div>
    </div>

    <el-dialog v-model="dialogVisible" :title="selected?.title ?? '消息详情'" width="640px">
      <div v-if="selected" class="space-y-4">
        <div class="flex flex-wrap items-center gap-2 text-xs text-[var(--text-secondary)]">
          <el-tag :type="MessageTypeMeta[selected.type].tagType" size="small" effect="light">
            {{ MessageTypeMeta[selected.type].label }}
          </el-tag>
          <el-tag :type="MessageLevelMeta[selected.level].tagType" size="small" effect="light">
            {{ MessageLevelMeta[selected.level].label }}
          </el-tag>
          <span>{{ formatTime(selected.created_at) }}</span>
        </div>
        <div class="whitespace-pre-wrap text-sm leading-relaxed text-[var(--text-primary)]">{{ selected.body }}</div>
        <el-button v-if="selected.link" type="primary" plain @click="openLink">查看关联页面</el-button>
      </div>
      <template #footer>
        <el-button @click="dialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>
