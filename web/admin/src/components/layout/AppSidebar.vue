<script setup lang="ts">
import { menuConfig, type MenuItem } from '@/config/menu'

defineProps<{
  activeMenu: string
  isCollapse: boolean
}>()

const hasChildren = (item: MenuItem) => Boolean(item.children?.length)
</script>

<template>
  <div class="flex h-full flex-col">
    <!-- 品牌区 -->
    <div
      class="flex h-[var(--header-height)] shrink-0 items-center gap-2.5 px-5"
      :class="{ 'justify-center px-0': isCollapse }"
    >
      <div
        class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-gradient-to-br from-brand-600 to-brand-500 shadow-sm"
      >
        <span class="i-ep-office-building text-base text-white" />
      </div>
      <span
        v-show="!isCollapse"
        class="truncate text-[15px] font-semibold tracking-wide text-[var(--text-primary)]"
      >
        Admin Console
      </span>
    </div>

    <!-- 导航菜单：图标必须用 el-icon 包裹，折叠态才会保留（Element Plus 官方契约） -->
    <nav class="min-h-0 flex-1 overflow-y-auto overflow-x-hidden py-2">
      <el-menu :default-active="activeMenu" :collapse="isCollapse" router>
        <template v-for="item in menuConfig" :key="item.path">
          <el-sub-menu v-if="hasChildren(item)" :index="item.path">
            <template #title>
              <el-icon v-if="item.icon" :size="16"><component :is="item.icon" /></el-icon>
              <span>{{ item.title }}</span>
            </template>
            <el-menu-item v-for="child in item.children" :key="child.path" :index="child.path">
              <el-icon v-if="child.icon" :size="16"><component :is="child.icon" /></el-icon>
              <span>{{ child.title }}</span>
            </el-menu-item>
          </el-sub-menu>

          <el-menu-item v-else :index="item.path">
            <el-icon v-if="item.icon" :size="16"><component :is="item.icon" /></el-icon>
            <span>{{ item.title }}</span>
          </el-menu-item>
        </template>
      </el-menu>
    </nav>

    <!-- 版本信息 -->
    <div v-show="!isCollapse" class="shrink-0 border-t border-line px-5 py-3">
      <div class="text-xs text-[var(--text-muted)]">go-web-template</div>
      <div class="mt-0.5 text-xs text-[var(--text-muted)]">Admin v0.1.0</div>
    </div>
  </div>
</template>
