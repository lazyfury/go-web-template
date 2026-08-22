<script setup lang="ts">
import { computed } from 'vue'
import { UserStatusMeta, type UserStatusValue } from '@/types/user'

/**
 * 用户状态标签：由 UserStatusMeta（types/user.ts）驱动展示文案与颜色。
 */
const props = defineProps<{
  status: UserStatusValue
}>()

const meta = computed(() => UserStatusMeta[props.status] ?? UserStatusMeta[0])
</script>

<template>
  <el-tag :type="meta.tagType" size="small" round effect="light">{{ meta.label }}</el-tag>
</template>
