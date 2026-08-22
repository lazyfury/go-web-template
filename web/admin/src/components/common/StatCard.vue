<script setup lang="ts">
/**
 * 统计卡片：标题 + 大数字 + 趋势 tag + 渐变图标底。
 * 纯展示组件，数据由父级传入（便于对接真实指标接口）。
 */
defineProps<{
  title: string
  value: string
  trend?: string
  positive?: boolean
  iconClass?: string
}>()
</script>

<template>
  <div class="app-card group relative overflow-hidden p-5 transition-shadow hover:shadow-card-hover">
    <div class="flex items-start justify-between">
      <div class="min-w-0">
        <div class="text-13px text-[var(--text-secondary)]">{{ title }}</div>
        <div class="num mt-2 truncate text-2xl font-semibold tracking-tight text-[var(--text-primary)]">
          {{ value }}
        </div>
        <div v-if="trend" class="mt-2 flex items-center gap-1">
          <span
            class="inline-flex items-center gap-0.5 rounded-full px-1.5 py-0.5 text-11px font-medium"
            :class="positive ? 'bg-[var(--success)]/10 text-[var(--success)]' : 'bg-[var(--danger)]/10 text-[var(--danger)]'"
          >
            <span :class="positive ? 'i-ep-caret-top' : 'i-ep-caret-bottom'" class="text-xs" />
            {{ trend }}
          </span>
          <span class="text-11px text-[var(--text-muted)]">较昨日</span>
        </div>
      </div>

      <div
        v-if="iconClass"
        class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-brand-500 to-brand-600 shadow-sm transition-transform group-hover:scale-105"
      >
        <span :class="iconClass" class="text-xl text-white" />
      </div>
    </div>
  </div>
</template>
