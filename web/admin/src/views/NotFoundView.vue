<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <div class="flex min-h-[60vh] flex-col items-center justify-center">
    <div class="relative">
      <div
        class="bg-gradient-to-br from-brand-500 to-brand-600 bg-clip-text text-[120px] leading-none font-bold text-transparent"
      >
        404
      </div>
      <div class="absolute -top-4 -right-4">
        <span class="i-ep-warning-filled text-2xl text-brand-300" />
      </div>
    </div>
    <div class="mt-6 text-lg font-semibold text-[var(--text-primary)]">页面不存在</div>
    <p class="mt-2 text-sm text-[var(--text-secondary)]">
      你访问的页面不存在或已被移除
    </p>
    <el-button type="primary" class="mt-8" @click="router.push('/dashboard')">
      <span class="i-ep-back mr-1" />返回首页
    </el-button>
  </div>
</template>
