<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authApi } from '@/api/auth'
import MessageBell from '@/components/layout/MessageBell.vue'
import { useAuthStore } from '@/stores/auth'

defineProps<{
  isCollapse: boolean
}>()

const emit = defineEmits<{
  toggleCollapse: []
}>()

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const pageTitle = computed(() => route.meta.title ?? '')
const userName = computed(() => authStore.user?.name ?? 'Admin')
const userRole = computed(() => (authStore.user?.role === 'admin' ? '管理员' : '成员'))
const userEmail = computed(() => authStore.user?.email ?? 'admin@admin.local')
const avatarText = computed(() => userName.value.slice(0, 1).toUpperCase())

const handleLogout = () => {
  authApi.logout()
  ElMessage.success('已退出登录')
  router.replace('/login')
}
</script>

<template>
  <div class="flex h-full items-center justify-between px-4">
    <div class="flex min-w-0 items-center gap-3">
      <button
        type="button"
        class="flex h-8 w-8 cursor-pointer items-center justify-center rounded-md text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
        :title="isCollapse ? '展开菜单' : '收起菜单'"
        @click="emit('toggleCollapse')"
      >
        <span :class="isCollapse ? 'i-ep-expand' : 'i-ep-fold'" class="text-base" />
      </button>

      <span class="truncate text-[15px] font-semibold text-[var(--text-primary)]">
        {{ pageTitle }}
      </span>
    </div>

    <div class="flex shrink-0 items-center gap-1">
      <MessageBell />

      <el-dropdown trigger="click" placement="bottom-end">
        <button
          type="button"
          class="ml-1 flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 transition-colors hover:bg-[var(--bg-subtle)]"
        >
          <span
            class="flex h-7 w-7 items-center justify-center rounded-full bg-gradient-to-br from-brand-500 to-brand-600 text-xs font-semibold text-white"
          >
            {{ avatarText }}
          </span>
          <span class="hidden text-13px font-medium text-[var(--text-primary)] sm:inline">
            {{ userName }}
          </span>
          <span class="i-ep-arrow-down hidden text-xs text-[var(--text-muted)] sm:inline" />
        </button>

        <template #dropdown>
          <el-dropdown-menu>
            <div class="border-b border-line px-4 py-3">
              <div class="text-sm font-semibold text-[var(--text-primary)]">{{ userName }}</div>
              <div class="mt-0.5 text-xs text-[var(--text-muted)]">{{ userRole }}</div>
              <div class="mt-0.5 text-xs text-[var(--text-muted)]">{{ userEmail }}</div>
            </div>
            <el-dropdown-item divided @click="handleLogout">
              <span class="i-ep-switch-button mr-1.5 text-sm text-[var(--danger)]" />
              <span class="text-[var(--danger)]">退出登录</span>
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </div>
</template>
