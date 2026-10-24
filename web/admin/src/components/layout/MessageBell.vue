<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useMessageStore } from '@/stores/messages'

const router = useRouter()
const messages = useMessageStore()

onMounted(() => messages.startPolling())
onBeforeUnmount(() => messages.stopPolling())

const openInbox = () => {
  router.push('/messages')
}
</script>

<template>
  <el-tooltip
    :content="messages.unreadCount > 0 ? `${messages.unreadCount} 条未读消息` : '消息中心'"
    placement="bottom"
  >
    <button
      type="button"
      class="flex h-8 w-8 cursor-pointer items-center justify-center rounded-md text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
      @click="openInbox"
    >
      <el-badge
        :value="messages.unreadCount"
        :max="99"
        :hidden="messages.unreadCount === 0"
        :offset="[-2, 2]"
      >
        <span class="i-ep-bell text-base" />
      </el-badge>
    </button>
  </el-tooltip>
</template>
