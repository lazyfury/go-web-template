<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useTabsStore } from '@/stores/tabs'

/**
 * 多标签工作区。手写 pill 样式（相对 el-tabs 自定义更可控、视觉更轻），
 * 标签列表由 tabs store 管理（含持久化）。
 */

defineProps<{
  activePath: string
}>()

const route = useRoute()
const router = useRouter()
const tabsStore = useTabsStore()

const tabs = computed(() => tabsStore.tabs)

const handleTabClick = (path: string) => {
  if (path !== route.path) {
    router.push(path)
  }
}

const handleTabClose = (path: string) => {
  tabsStore.remove(path)
  if (route.path === path) {
    const next = tabsStore.tabs[tabsStore.tabs.length - 1]
    router.push(next?.path ?? '/dashboard')
  }
}

const handleCloseOthers = () => {
  tabsStore.closeOthers(route.path)
}

const handleCloseAll = () => {
  tabsStore.closeAll()
  if (route.path !== '/dashboard') {
    router.push('/dashboard')
  }
}
</script>

<template>
  <div class="flex h-full items-center gap-2 pl-4 pr-3">
    <div class="app-tabs-scroll flex min-w-0 flex-1 items-center gap-1.5">
      <button
        v-for="tab in tabs"
        :key="tab.path"
        type="button"
        class="group flex h-7 shrink-0 cursor-pointer items-center gap-1 rounded-md border px-3 text-12px transition-colors"
        :class="
          tab.path === activePath
            ? 'border-brand-200 bg-brand-50 font-medium text-brand-600'
            : 'border-line bg-white text-[var(--text-secondary)] hover:border-[var(--line-strong)] hover:text-[var(--text-primary)]'
        "
        @click="handleTabClick(tab.path)"
      >
        <span
          v-if="tab.path === activePath"
          class="h-1.5 w-1.5 rounded-full bg-brand-500"
        />
        <span>{{ tab.title }}</span>
        <span
          v-if="tab.closable"
          class="i-ep-close ml-0.5 cursor-pointer rounded-sm text-xs text-[var(--text-muted)] transition-colors hover:bg-black/10 hover:text-[var(--danger)]"
          @click.stop="handleTabClose(tab.path)"
        />
      </button>
    </div>

    <el-dropdown trigger="click">
      <button
        type="button"
        class="flex h-7 shrink-0 cursor-pointer items-center gap-1 rounded-md px-2 text-12px text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
      >
        <span class="i-ep-arrow-down text-xs" />
        标签操作
      </button>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item @click="handleCloseOthers">关闭其他标签</el-dropdown-item>
          <el-dropdown-item @click="handleCloseAll">关闭全部标签</el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>
  </div>
</template>

<style scoped>
.app-tabs-scroll {
  scrollbar-width: none;
}
.app-tabs-scroll::-webkit-scrollbar {
  display: none;
}
</style>
