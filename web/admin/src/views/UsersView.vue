<script setup lang="ts">
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import StatusTag from '@/components/common/StatusTag.vue'
import { usePagination } from '@/composables/usePagination'
import { userApi } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types/user'
import { notifyError } from '@/utils/feedback'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('users:write'))
const canDelete = computed(() => auth.can('users:delete'))

/**
 * 用户管理：列表（关键字 + 分页）+ 新建 + 编辑 + 删除。
 * 数据全部对接真实后端 /api/v1/users。
 */

const { page, pageSize, total, loading, reset, onPageChange, onPageSizeChange } = usePagination()

const keyword = ref('')
const users = ref<User[]>([])

const loadUsers = async () => {
  loading.value = true
  try {
    const result = await userApi.list({
      page: page.value,
      page_size: pageSize.value,
      keyword: keyword.value.trim() || undefined,
    })
    users.value = result.items
    total.value = result.total
  } catch (error) {
    notifyError(error, '加载用户列表失败')
  } finally {
    loading.value = false
  }
}

const onSearch = async () => {
  reset()
  await loadUsers()
}

const onReset = async () => {
  keyword.value = ''
  reset()
  await loadUsers()
}

/* ── 新建 / 编辑 ─────────────────────────────────────── */

const dialogVisible = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const editingId = ref<number | null>(null)
const submitting = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  name: '',
  email: '',
  password: '',
  role: 'member' as User['role'],
})

const rules: FormRules = {
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 2, max: 64, message: '长度 2-64 个字符', trigger: 'blur' },
  ],
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { type: 'email', message: '邮箱格式不正确', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入初始密码', trigger: 'blur' },
    { min: 8, max: 72, message: '密码长度 8-72 位', trigger: 'blur' },
  ],
}

const openCreateDialog = () => {
  dialogMode.value = 'create'
  editingId.value = null
  form.name = ''
  form.email = ''
  form.password = ''
  form.role = 'member'
  dialogVisible.value = true
}

const openEditDialog = (row: User) => {
  dialogMode.value = 'edit'
  editingId.value = row.id
  form.name = row.name
  form.email = row.email
  form.password = ''
  form.role = row.role
  dialogVisible.value = true
}

const submitForm = async () => {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    if (dialogMode.value === 'create') {
      await userApi.create({
        name: form.name,
        email: form.email,
        password: form.password,
        role: form.role,
      })
      ElMessage.success('创建成功')
      reset()
    } else if (editingId.value !== null) {
      await userApi.update(editingId.value, { name: form.name, email: form.email, role: form.role })
      ElMessage.success('保存成功')
    }
    dialogVisible.value = false
    await loadUsers()
  } catch (error) {
    notifyError(error, dialogMode.value === 'create' ? '创建失败' : '保存失败')
  } finally {
    submitting.value = false
  }
}

/* ── 删除 ────────────────────────────────────────────── */

const handleDelete = async (row: User) => {
  try {
    await ElMessageBox.confirm(`确定删除用户「${row.name}」吗？删除后不可恢复。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      confirmButtonClass: 'el-button--danger',
    })
  } catch {
    return // 用户取消
  }

  try {
    await userApi.remove(row.id)
    ElMessage.success('删除成功')
    await loadUsers()
  } catch (error) {
    notifyError(error, '删除失败')
  }
}

const formatDate = (value: string) => value.slice(0, 10)

onMounted(loadUsers)
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <div class="app-card">
      <!-- 工具栏 -->
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-5 py-4">
        <div class="flex flex-wrap items-center gap-2">
          <el-input
            v-model="keyword"
            placeholder="搜索用户名 / 邮箱"
            clearable
            class="w-64"
            :prefix-icon="'Search'"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
          <el-button type="primary" @click="onSearch">
            <span class="i-ep-search mr-1" />搜索
          </el-button>
          <el-button @click="onReset">
            <span class="i-ep-refresh-left mr-1" />重置
          </el-button>
        </div>
        <el-button v-if="canWrite" type="primary" @click="openCreateDialog">
          <span class="i-ep-plus mr-1" />新建用户
        </el-button>
      </div>

      <!-- 表格 -->
      <el-table v-loading="loading" :data="users" class="px-2">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="用户名" min-width="140">
          <template #default="{ row }">
            <span class="font-medium text-[var(--text-primary)]">{{ row.name }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="email" label="邮箱" min-width="220" />
        <el-table-column label="角色" width="100">
          <template #default="{ row }">
            <el-tag :type="row.role === 'admin' ? 'warning' : 'info'" size="small">
              {{ row.role === 'admin' ? '管理员' : '成员' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <StatusTag :status="row.status" />
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="140">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button v-if="canWrite" link type="primary" @click="openEditDialog(row)">编辑</el-button>
            <el-button v-if="canDelete" link type="danger" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无用户数据" :image-size="80" />
        </template>
      </el-table>

      <!-- 分页 -->
      <div class="flex justify-end px-5 py-4">
        <el-pagination
          :current-page="page"
          :page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @update:current-page="onPageChange"
          @update:page-size="onPageSizeChange"
          @current-change="loadUsers"
          @size-change="loadUsers"
        />
      </div>
    </div>

    <!-- 新建 / 编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogMode === 'create' ? '新建用户' : '编辑用户'"
      width="480px"
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item label="用户名" prop="name">
          <el-input v-model="form.name" placeholder="2-64 个字符" clearable />
        </el-form-item>
        <el-form-item label="邮箱" prop="email">
          <el-input v-model="form.email" placeholder="user@example.com" clearable />
        </el-form-item>
        <el-form-item v-if="dialogMode === 'create'" label="初始密码" prop="password">
          <el-input v-model="form.password" type="password" placeholder="8-72 位，交付后请让对方自行修改" show-password />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.role" class="w-full">
            <el-option label="成员（仅自己）" value="member" />
            <el-option label="管理员（全部权限）" value="admin" />
          </el-select>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>
