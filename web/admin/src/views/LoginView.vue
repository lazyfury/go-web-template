<script setup lang="ts">
import type { FormInstance, FormRules } from 'element-plus'
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authApi } from '@/api/auth'
import { notifyError } from '@/utils/feedback'
import { safeRedirectTarget } from '@/utils/redirect'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  email: '',
  password: '',
})

const rules: FormRules = {
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { type: 'email', message: '邮箱格式不正确', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 8, message: '密码至少 8 位', trigger: 'blur' },
  ],
}

const handleLogin = async () => {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  loading.value = true
  try {
    await authApi.login({ email: form.email.trim(), password: form.password })
    ElMessage.success('登录成功')
    // 只接受站内路径：`?redirect=//evil.com` 之类一律回仪表盘。
    router.replace(safeRedirectTarget(route.query.redirect))
  } catch (error) {
    notifyError(error, '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-screen">
    <!-- 品牌区（小屏隐藏） -->
    <div class="relative hidden flex-1 overflow-hidden lg:block">
      <div class="absolute inset-0 bg-gradient-to-br from-[#312e81] via-[#4f46e5] to-[#7c3aed]" />
      <!-- 装饰光斑 -->
      <div class="absolute -top-32 -left-32 h-96 w-96 rounded-full bg-white/10 blur-3xl" />
      <div class="absolute right-0 bottom-0 h-80 w-80 translate-x-1/3 translate-y-1/3 rounded-full bg-white/5 blur-3xl" />
      <div
        class="absolute top-1/4 right-16 h-40 w-40 rounded-full border border-white/15"
      />
      <div class="absolute top-1/3 right-8 h-64 w-64 rounded-full border border-white/10" />

      <!-- 品牌内容 -->
      <div class="relative flex h-full flex-col justify-center px-16 xl:px-24">
        <div class="flex items-center gap-3">
          <div class="flex h-11 w-11 items-center justify-center rounded-xl bg-white/15 backdrop-blur">
            <span class="i-ep-office-building text-2xl text-white" />
          </div>
          <span class="text-xl font-semibold tracking-wide text-white">Admin Console</span>
        </div>

        <h1 class="mt-10 text-4xl leading-tight font-bold text-white xl:text-5xl">
          Go Web 模板<br />后台管理系统
        </h1>
        <p class="mt-5 max-w-md text-15px leading-relaxed text-white/70">
          基于 go-web-template 的前端模板：分层清晰、统一响应信封、开箱即用的
          CRUD 与仪表盘，风格与工程范式一并沉淀。
        </p>

        <ul class="mt-10 space-y-4">
          <li v-for="feature in ['分层架构，接口驱动可独立测试', '统一响应与业务错误码', 'Vue 3 + TS + Element Plus + UnoCSS']" :key="feature" class="flex items-center gap-3 text-sm text-white/85">
            <span class="flex h-5 w-5 items-center justify-center rounded-full bg-white/15">
              <span class="i-ep-check text-xs text-white" />
            </span>
            {{ feature }}
          </li>
        </ul>
      </div>
    </div>

    <!-- 表单区 -->
    <div class="flex w-full items-center justify-center bg-[var(--bg-page)] px-6 py-12 lg:w-[520px] lg:shrink-0">
      <div class="w-full max-w-360">
        <!-- 移动端品牌 -->
        <div class="mb-8 flex items-center gap-2.5 lg:hidden">
          <div class="flex h-9 w-9 items-center justify-center rounded-lg bg-gradient-to-br from-brand-500 to-brand-600">
            <span class="i-ep-office-building text-lg text-white" />
          </div>
          <span class="text-lg font-semibold text-[var(--text-primary)]">Admin Console</span>
        </div>

        <h2 class="text-2xl font-bold text-[var(--text-primary)]">欢迎回来</h2>
        <p class="mt-2 text-sm text-[var(--text-secondary)]">登录后台管理系统</p>

        <el-form ref="formRef" :model="form" :rules="rules" label-position="top" size="large" class="mt-8" @submit.prevent="handleLogin">
          <el-form-item label="邮箱" prop="email">
            <el-input v-model="form.email" placeholder="admin@example.com" clearable :prefix-icon="'Message'" />
          </el-form-item>

          <el-form-item label="密码" prop="password">
            <el-input v-model="form.password" type="password" placeholder="请输入密码" show-password :prefix-icon="'Lock'" />
          </el-form-item>

          <el-button type="primary" class="mt-2 w-full" size="large" :loading="loading" native-type="submit">
            登 录
          </el-button>
        </el-form>

        <!-- mock 模式提示 -->
        <div class="mt-6 flex items-start gap-2 rounded-lg border border-brand-100 bg-brand-50 px-3 py-2.5">
          <span class="i-ep-info-filled mt-0.5 shrink-0 text-sm text-brand-500" />
          <p class="text-12px leading-relaxed text-brand-700">
            当前为<strong>演示模式</strong>：后端尚未提供认证接口，任意用户名 + 4 位以上密码即可登录。
            接入真实接口见 <code>src/api/auth.ts</code>。
          </p>
        </div>
      </div>
    </div>
  </div>
</template>
