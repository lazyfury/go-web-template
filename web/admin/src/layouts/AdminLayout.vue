<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppSidebar from '@/components/layout/AppSidebar.vue'
import AppHeader from '@/components/layout/AppHeader.vue'
import AppTabs from '@/components/layout/AppTabs.vue'
import { useTabsStore, type WorkTab } from '@/stores/tabs'

/**
 * 后台主布局：侧栏 + 头部 + 多标签工作区 + 内容区。
 * 布局尺寸统一走 styles/tokens.css 中的 CSS 变量，折叠状态持久化。
 */

const COLLAPSE_KEY = 'gw_admin_sidebar_collapsed'

const route = useRoute()
const tabsStore = useTabsStore()

const isCollapse = ref(localStorage.getItem(COLLAPSE_KEY) === '1')
// 必须用 computed：随 isCollapse 响应式变化，否则收起/展开时侧栏宽度不更新
const asideWidth = computed(() =>
  isCollapse.value ? 'var(--sidebar-width-collapsed)' : 'var(--sidebar-width)',
)
const activeTabPath = route.path

const toggleCollapse = () => {
  isCollapse.value = !isCollapse.value
  localStorage.setItem(COLLAPSE_KEY, isCollapse.value ? '1' : '0')
}

/** 路由变化 → 同步多标签。 */
watch(
  () => route.path,
  (path) => {
    if (!route.meta.title) return
    const tab: WorkTab = {
      path,
      title: route.meta.title,
      closable: path !== '/dashboard',
    }
    tabsStore.upsert(tab)
  },
  { immediate: true },
)

onMounted(() => {
  tabsStore.ensureDashboard()
})
</script>

<template>
  <div class="flex h-full overflow-hidden">
    <aside
      class="flex flex-col shrink-0 border-r border-line bg-white transition-[width] duration-200"
      :style="{ width: asideWidth }"
    >
      <AppSidebar :active-menu="activeTabPath" :is-collapse="isCollapse" />
    </aside>

    <div class="flex min-w-0 flex-1 flex-col">
      <header class="z-20 border-b border-line bg-white" style="height: var(--header-height)">
        <AppHeader :is-collapse="isCollapse" @toggle-collapse="toggleCollapse" />
      </header>

      <div class="z-10 border-b border-line bg-white" style="height: var(--tabs-height)">
        <AppTabs :active-path="activeTabPath" />
      </div>

      <main class="min-h-0 flex-1 overflow-y-auto">
        <router-view />
      </main>
    </div>
  </div>
</template>
