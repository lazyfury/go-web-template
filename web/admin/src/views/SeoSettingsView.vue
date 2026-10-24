<script setup lang="ts">
import { onMounted } from 'vue'
import { useSettingGroup } from '@/composables/useSettingGroup'

/** SEO 设置：整组读写字典分组 site.seo。 */
const GROUP = 'site.seo'

const robotsOptions = [
  { label: 'index, follow（允许收录）', value: 'index,follow' },
  { label: 'noindex, nofollow（完全不收录）', value: 'noindex,nofollow' },
  { label: 'index, nofollow（收录但不跟随链接）', value: 'index,nofollow' },
  { label: 'noindex, follow（不收录但跟随链接）', value: 'noindex,follow' },
]

const { data, loading, saving, exists, load, save } = useSettingGroup(GROUP, {
  title: '',
  keywords: '',
  description: '',
  robots: 'index,follow',
  og_image: '',
})

onMounted(load)
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <div class="app-card mx-auto max-w-4xl overflow-hidden">
      <div class="flex flex-wrap items-start justify-between gap-3 border-b border-line px-5 py-4">
        <div>
          <div class="flex items-center gap-2">
            <h2 class="text-base font-semibold">SEO 设置</h2>
            <el-tag v-if="!exists" size="small" type="info" effect="light">尚未创建</el-tag>
          </div>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">
            默认标题、关键词、描述与 robots；保存后写入字典分组
            <code class="font-mono">site.seo</code>。
          </p>
        </div>
        <div class="flex gap-2">
          <el-button :disabled="loading || saving" @click="load">重新加载</el-button>
          <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        </div>
      </div>

      <el-form v-loading="loading" label-position="top" class="p-5">
        <el-form-item label="默认标题">
          <el-input v-model="data.title" maxlength="128" show-word-limit placeholder="如 Example · 技术博客" />
          <p class="mt-1 text-xs text-[var(--text-muted)]">建议 60 个字符以内，页面可覆盖。</p>
        </el-form-item>

        <el-form-item label="关键词">
          <el-input
            v-model="data.keywords"
            type="textarea"
            :rows="2"
            maxlength="1024"
            show-word-limit
            placeholder="用英文逗号分隔，如 go,gin,gorm"
          />
        </el-form-item>

        <el-form-item label="默认描述">
          <el-input
            v-model="data.description"
            type="textarea"
            :rows="4"
            maxlength="1024"
            show-word-limit
            placeholder="建议 120–160 个字符，未单独设置描述的页面会使用这里"
          />
        </el-form-item>

        <div class="grid gap-x-4 md:grid-cols-2">
          <el-form-item label="Robots">
            <el-select v-model="data.robots" class="w-full">
              <el-option v-for="option in robotsOptions" :key="option.value" :label="option.label" :value="option.value" />
            </el-select>
          </el-form-item>
          <el-form-item label="社交分享图（og:image）">
            <el-input v-model="data.og_image" placeholder="https://cdn.example.com/og.png" />
          </el-form-item>
        </div>
      </el-form>

      <div class="border-t border-line bg-[var(--bg-subtle)] px-5 py-3 text-xs text-[var(--text-secondary)]">
        数据由 dict API 提供；可在「字典管理」中查看或批量维护 group
        <code class="font-mono">site.seo</code>。
      </div>
    </div>
  </div>
</template>
