<script setup lang="ts">
import { onMounted } from 'vue'
import { useSettingGroup } from '@/composables/useSettingGroup'

/** 基础设置：整组读写字典分组 site.basic。 */
const GROUP = 'site.basic'

const { data, loading, saving, exists, load, save } = useSettingGroup(GROUP, {
  site_name: '',
  site_url: '',
  site_logo: '',
  site_description: '',
  contact_email: '',
  icp: '',
  copyright: '',
})

onMounted(load)
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <div class="app-card mx-auto max-w-4xl overflow-hidden">
      <div class="flex flex-wrap items-start justify-between gap-3 border-b border-line px-5 py-4">
        <div>
          <div class="flex items-center gap-2">
            <h2 class="text-base font-semibold">基础设置</h2>
            <el-tag v-if="!exists" size="small" type="info" effect="light">尚未创建</el-tag>
          </div>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">
            站点名称、地址、Logo 与联系方式；保存后写入字典分组
            <code class="font-mono">site.basic</code>。
          </p>
        </div>
        <div class="flex gap-2">
          <el-button :disabled="loading || saving" @click="load">重新加载</el-button>
          <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        </div>
      </div>

      <el-form v-loading="loading" label-position="top" class="p-5">
        <div class="grid gap-x-4 md:grid-cols-2">
          <el-form-item label="站点名称">
            <el-input v-model="data.site_name" placeholder="如 Example Blog" />
          </el-form-item>
          <el-form-item label="站点地址">
            <el-input v-model="data.site_url" placeholder="https://example.com" />
          </el-form-item>
          <el-form-item label="Logo URL">
            <el-input v-model="data.site_logo" placeholder="https://cdn.example.com/logo.png" />
          </el-form-item>
          <el-form-item label="联系邮箱">
            <el-input v-model="data.contact_email" placeholder="contact@example.com" />
          </el-form-item>
          <el-form-item label="备案号">
            <el-input v-model="data.icp" placeholder="如 京ICP备00000000号" />
          </el-form-item>
          <el-form-item label="版权信息">
            <el-input v-model="data.copyright" placeholder="© 2026 Example" />
          </el-form-item>
        </div>

        <el-form-item label="站点简介">
          <el-input
            v-model="data.site_description"
            type="textarea"
            :rows="4"
            maxlength="1024"
            show-word-limit
            placeholder="用于页脚、关于页或 SEO 兜底描述"
          />
        </el-form-item>
      </el-form>

      <div class="border-t border-line bg-[var(--bg-subtle)] px-5 py-3 text-xs text-[var(--text-secondary)]">
        数据由 dict API 提供；可在「字典管理」中查看或批量维护 group
        <code class="font-mono">site.basic</code>。
      </div>
    </div>
  </div>
</template>
