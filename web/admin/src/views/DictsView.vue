<script setup lang="ts">
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import { dictApi } from '@/api/dict'
import { useAuthStore } from '@/stores/auth'
import {
  DictStatusMeta,
  DictValueTypeMeta,
  type DictGroup,
  type DictItem,
} from '@/types/dict'
import { notifyError } from '@/utils/feedback'

/**
 * 字典/配置管理。
 * 左侧维护 group（= setting），右侧维护该 group 的 dict array（items）。
 * 数据全部对接真实后端 /api/v1/dict-groups 与 /api/v1/dict-items。
 */

const auth = useAuthStore()
const canWrite = computed(() => auth.can('dicts:write'))

/* ── 分组列表 ────────────────────────────────────────── */

const groups = ref<DictGroup[]>([])
const groupTotal = ref(0)
const groupPage = ref(1)
const groupPageSize = 10
const groupKeyword = ref('')
const groupsLoading = ref(false)
const selectedGroup = ref<DictGroup | null>(null)

const items = ref<DictItem[]>([])
const itemsLoading = ref(false)

const loadItems = async () => {
  if (!selectedGroup.value) {
    items.value = []
    return
  }
  itemsLoading.value = true
  try {
    items.value = await dictApi.listItems(selectedGroup.value.id, true)
  } catch (error) {
    notifyError(error, '加载字典项失败')
  } finally {
    itemsLoading.value = false
  }
}

const selectGroup = async (group: DictGroup) => {
  selectedGroup.value = group
  await loadItems()
}

const loadGroups = async (keepSelection = true) => {
  groupsLoading.value = true
  try {
    const result = await dictApi.listGroups({
      page: groupPage.value,
      page_size: groupPageSize,
      keyword: groupKeyword.value.trim() || undefined,
    })
    groups.value = result.items
    groupTotal.value = result.total

    if (keepSelection && selectedGroup.value) {
      const refreshed = result.items.find((group) => group.id === selectedGroup.value?.id)
      if (refreshed) {
        selectedGroup.value = refreshed
        return
      }
    }

    const first = result.items[0]
    if (first) {
      await selectGroup(first)
    } else {
      selectedGroup.value = null
      items.value = []
    }
  } catch (error) {
    notifyError(error, '加载字典分组失败')
  } finally {
    groupsLoading.value = false
  }
}

const onGroupSearch = async () => {
  groupPage.value = 1
  await loadGroups(false)
}

const onGroupPageChange = async (next: number) => {
  groupPage.value = next
  await loadGroups(false)
}

/* ── 新建 / 编辑分组 ─────────────────────────────────── */

const groupDialogVisible = ref(false)
const groupDialogMode = ref<'create' | 'edit'>('create')
const groupSubmitting = ref(false)
const groupFormRef = ref<FormInstance>()

const groupForm = reactive({
  key: '',
  name: '',
  description: '',
  status: 1 as DictGroup['status'],
})

const groupRules: FormRules = {
  key: [
    { required: true, message: '请输入分组 key', trigger: 'blur' },
    {
      pattern: /^[a-z][a-z0-9_.-]{0,63}$/,
      message: '小写字母开头，可含数字、下划线、点、连字符，最长 64',
      trigger: 'blur',
    },
  ],
  name: [
    { required: true, message: '请输入分组名称', trigger: 'blur' },
    { max: 64, message: '最多 64 个字符', trigger: 'blur' },
  ],
  description: [{ max: 255, message: '最多 255 个字符', trigger: 'blur' }],
}

const openCreateGroup = () => {
  groupDialogMode.value = 'create'
  groupForm.key = ''
  groupForm.name = ''
  groupForm.description = ''
  groupForm.status = 1
  groupDialogVisible.value = true
}

const openEditGroup = (group: DictGroup) => {
  groupDialogMode.value = 'edit'
  groupForm.key = group.key
  groupForm.name = group.name
  groupForm.description = group.description
  groupForm.status = group.status
  groupDialogVisible.value = true
}

const submitGroup = async () => {
  if (!groupFormRef.value) return
  const valid = await groupFormRef.value.validate().catch(() => false)
  if (!valid) return

  groupSubmitting.value = true
  try {
    if (groupDialogMode.value === 'create') {
      await dictApi.createGroup({
        key: groupForm.key,
        name: groupForm.name,
        description: groupForm.description,
        status: groupForm.status,
      })
      ElMessage.success('创建成功')
      groupDialogVisible.value = false
      groupKeyword.value = ''
      groupPage.value = 1
      await loadGroups(false)
    } else if (selectedGroup.value) {
      const updated = await dictApi.updateGroup(selectedGroup.value.id, {
        name: groupForm.name,
        description: groupForm.description,
        status: groupForm.status,
      })
      ElMessage.success('保存成功')
      groupDialogVisible.value = false
      selectedGroup.value = updated
      await loadGroups()
    }
  } catch (error) {
    notifyError(error, groupDialogMode.value === 'create' ? '创建分组失败' : '保存分组失败')
  } finally {
    groupSubmitting.value = false
  }
}

const handleDeleteGroup = async () => {
  const group = selectedGroup.value
  if (!group) return
  try {
    await ElMessageBox.confirm(
      `确定删除分组「${group.name}」吗？其下所有字典项会一并软删除。`,
      '删除确认',
      {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消',
        confirmButtonClass: 'el-button--danger',
      },
    )
  } catch {
    return // 用户取消
  }

  try {
    await dictApi.removeGroup(group.id)
    ElMessage.success('删除成功')
    selectedGroup.value = null
    items.value = []
    await loadGroups(false)
  } catch (error) {
    notifyError(error, '删除分组失败')
  }
}

/* ── 新建 / 编辑字典项 ───────────────────────────────── */

const itemDialogVisible = ref(false)
const itemDialogMode = ref<'create' | 'edit'>('create')
const editingItemId = ref<number | null>(null)
const itemSubmitting = ref(false)
const itemFormRef = ref<FormInstance>()

const itemForm = reactive({
  key: '',
  label: '',
  value: '',
  value_type: 'string' as DictItem['value_type'],
  sort: 0,
  status: 1 as DictItem['status'],
  remark: '',
})

const itemRules: FormRules = {
  key: [
    { required: true, message: '请输入 item key', trigger: 'blur' },
    { max: 64, message: '最多 64 个字符', trigger: 'blur' },
  ],
  label: [{ max: 128, message: '最多 128 个字符', trigger: 'blur' }],
  value: [{ max: 1024, message: '最多 1024 个字符', trigger: 'blur' }],
  remark: [{ max: 255, message: '最多 255 个字符', trigger: 'blur' }],
}

const nextSort = () => {
  if (items.value.length === 0) return 0
  return Math.max(...items.value.map((item) => item.sort)) + 10
}

const openCreateItem = () => {
  itemDialogMode.value = 'create'
  editingItemId.value = null
  itemForm.key = ''
  itemForm.label = ''
  itemForm.value = ''
  itemForm.value_type = 'string'
  itemForm.sort = nextSort()
  itemForm.status = 1
  itemForm.remark = ''
  itemDialogVisible.value = true
}

const openEditItem = (item: DictItem) => {
  itemDialogMode.value = 'edit'
  editingItemId.value = item.id
  itemForm.key = item.key
  itemForm.label = item.label
  itemForm.value = item.value
  itemForm.value_type = item.value_type
  itemForm.sort = item.sort
  itemForm.status = item.status
  itemForm.remark = item.remark
  itemDialogVisible.value = true
}

const submitItem = async () => {
  if (!selectedGroup.value || !itemFormRef.value) return
  const valid = await itemFormRef.value.validate().catch(() => false)
  if (!valid) return

  const payload = {
    key: itemForm.key,
    label: itemForm.label,
    value: itemForm.value,
    value_type: itemForm.value_type,
    sort: itemForm.sort,
    status: itemForm.status,
    remark: itemForm.remark,
  }

  itemSubmitting.value = true
  try {
    if (itemDialogMode.value === 'create') {
      await dictApi.createItem(selectedGroup.value.id, payload)
      ElMessage.success('创建成功')
    } else if (editingItemId.value !== null) {
      await dictApi.updateItem(editingItemId.value, payload)
      ElMessage.success('保存成功')
    }
    itemDialogVisible.value = false
    await Promise.all([loadItems(), loadGroups()])
  } catch (error) {
    notifyError(error, itemDialogMode.value === 'create' ? '创建字典项失败' : '保存字典项失败')
  } finally {
    itemSubmitting.value = false
  }
}

const handleDeleteItem = async (item: DictItem) => {
  try {
    await ElMessageBox.confirm(`确定删除字典项「${item.label}」吗？`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      confirmButtonClass: 'el-button--danger',
    })
  } catch {
    return // 用户取消
  }

  try {
    await dictApi.removeItem(item.id)
    ElMessage.success('删除成功')
    await Promise.all([loadItems(), loadGroups()])
  } catch (error) {
    notifyError(error, '删除字典项失败')
  }
}

const valueTypeLabel = (valueType: DictItem['value_type']) =>
  DictValueTypeMeta[valueType] ?? valueType

const formatValue = (item: DictItem) => {
  if (item.value_type !== 'json') return item.value
  try {
    return JSON.stringify(JSON.parse(item.value))
  } catch {
    return item.value
  }
}

onMounted(async () => {
  await loadGroups(false)
})
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <div class="grid gap-4 lg:grid-cols-[320px_1fr]">
      <!-- 左侧：分组列表（每个 group 就是一个 setting） -->
      <div class="app-card flex min-h-[620px] flex-col overflow-hidden">
        <div class="border-b border-line px-5 py-4">
          <div class="flex items-center justify-between gap-2">
            <div>
              <h2 class="text-base font-semibold">字典分组</h2>
              <p class="mt-1 text-xs text-[var(--text-secondary)]">group = setting</p>
            </div>
            <el-button v-if="canWrite" type="primary" size="small" @click="openCreateGroup">
              <span class="i-ep-plus mr-1" />新建
            </el-button>
          </div>
          <el-input
            v-model="groupKeyword"
            class="mt-3"
            placeholder="搜索 key / 名称"
            clearable
            @keyup.enter="onGroupSearch"
            @clear="onGroupSearch"
          >
            <template #prefix><span class="i-ep-search" /></template>
          </el-input>
        </div>

        <div v-loading="groupsLoading" class="flex-1 overflow-y-auto p-2">
          <button
            v-for="group in groups"
            :key="group.id"
            type="button"
            class="mb-2 w-full rounded-[10px] border px-3 py-2.5 text-left transition"
            :class="
              selectedGroup?.id === group.id
                ? 'border-[var(--brand-300)] bg-[var(--brand-50)]'
                : 'border-transparent hover:border-[var(--line-strong)] hover:bg-[var(--bg-subtle)]'
            "
            @click="selectGroup(group)"
          >
            <div class="flex items-center justify-between gap-2">
              <span class="truncate font-medium">{{ group.name }}</span>
              <el-tag :type="DictStatusMeta[group.status].tagType" size="small" effect="light">
                {{ DictStatusMeta[group.status].label }}
              </el-tag>
            </div>
            <div class="mt-1 truncate font-mono text-xs text-[var(--text-muted)]">{{ group.key }}</div>
            <div class="mt-1 text-xs text-[var(--text-secondary)]">{{ group.item_count }} 个选项</div>
          </button>
          <el-empty
            v-if="!groupsLoading && groups.length === 0"
            description="暂无字典分组"
            :image-size="64"
          />
        </div>

        <div class="flex justify-center border-t border-line px-3 py-2">
          <el-pagination
            small
            layout="prev, pager, next"
            :current-page="groupPage"
            :page-size="groupPageSize"
            :total="groupTotal"
            @current-change="onGroupPageChange"
          />
        </div>
      </div>

      <!-- 右侧：选中分组的 dict array -->
      <div class="app-card flex min-h-[620px] flex-col overflow-hidden">
        <template v-if="selectedGroup">
          <div class="flex flex-wrap items-start justify-between gap-3 border-b border-line px-5 py-4">
            <div>
              <div class="flex items-center gap-2">
                <h2 class="text-base font-semibold">{{ selectedGroup.name }}</h2>
                <el-tag :type="DictStatusMeta[selectedGroup.status].tagType" size="small" effect="light">
                  {{ DictStatusMeta[selectedGroup.status].label }}
                </el-tag>
              </div>
              <p class="mt-1 font-mono text-xs text-[var(--text-muted)]">{{ selectedGroup.key }}</p>
              <p class="mt-1 text-xs text-[var(--text-secondary)]">
                {{ selectedGroup.description || '暂无描述' }}
              </p>
            </div>
            <div class="flex flex-wrap gap-2">
              <el-button v-if="canWrite" @click="openEditGroup(selectedGroup)">
                <span class="i-ep-edit mr-1" />编辑分组
              </el-button>
              <el-button v-if="canWrite" type="primary" @click="openCreateItem">
                <span class="i-ep-plus mr-1" />新增选项
              </el-button>
              <el-button v-if="canWrite" type="danger" plain @click="handleDeleteGroup">
                <span class="i-ep-delete mr-1" />删除分组
              </el-button>
            </div>
          </div>

          <el-table v-loading="itemsLoading" :data="items" class="flex-1">
            <el-table-column prop="sort" label="排序" width="80" />
            <el-table-column label="Key" min-width="140">
              <template #default="{ row }">
                <span class="font-mono text-[var(--text-primary)]">{{ row.key }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="label" label="展示文案" min-width="140" />
            <el-table-column label="值" min-width="200">
              <template #default="{ row }">
                <span class="break-all font-mono text-xs text-[var(--text-secondary)]">{{ formatValue(row) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="值类型" width="100">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ valueTypeLabel(row.value_type) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small" effect="light">
                  {{ row.status === 1 ? '启用' : '停用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="remark" label="备注" min-width="140" show-overflow-tooltip />
            <el-table-column label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button v-if="canWrite" link type="primary" @click="openEditItem(row)">编辑</el-button>
                <el-button v-if="canWrite" link type="danger" @click="handleDeleteItem(row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="该分组还没有字典项" :image-size="72" />
            </template>
          </el-table>
        </template>
        <el-empty v-else description="请选择左侧字典分组" class="m-auto" :image-size="96" />
      </div>
    </div>

    <!-- 新建 / 编辑分组 -->
    <el-dialog
      v-model="groupDialogVisible"
      :title="groupDialogMode === 'create' ? '新建字典分组' : '编辑字典分组'"
      width="520px"
      :close-on-click-modal="false"
    >
      <el-form ref="groupFormRef" :model="groupForm" :rules="groupRules" label-position="top">
        <el-form-item label="分组 Key" prop="key">
          <el-input
            v-model="groupForm.key"
            :disabled="groupDialogMode === 'edit'"
            placeholder="如 user.status（创建后不可修改）"
          />
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="groupForm.name" placeholder="如 用户状态" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input
            v-model="groupForm.description"
            type="textarea"
            :rows="3"
            maxlength="255"
            show-word-limit
            placeholder="这个 setting 的用途"
          />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="groupForm.status" class="w-full">
            <el-option label="启用（消费侧可见）" :value="1" />
            <el-option label="停用（仅管理端可见）" :value="0" />
          </el-select>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="groupDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="groupSubmitting" @click="submitGroup">保存</el-button>
      </template>
    </el-dialog>

    <!-- 新建 / 编辑字典项 -->
    <el-dialog
      v-model="itemDialogVisible"
      :title="itemDialogMode === 'create' ? '新增字典项' : '编辑字典项'"
      width="560px"
      :close-on-click-modal="false"
    >
      <el-form ref="itemFormRef" :model="itemForm" :rules="itemRules" label-position="top">
        <div class="grid gap-x-3 md:grid-cols-2">
          <el-form-item label="Key" prop="key">
            <el-input v-model="itemForm.key" placeholder="如 1 / active / zh-CN" />
          </el-form-item>
          <el-form-item label="展示文案" prop="label">
            <el-input v-model="itemForm.label" placeholder="留空则与 Key 相同" />
          </el-form-item>
        </div>
        <div class="grid gap-x-3 md:grid-cols-2">
          <el-form-item label="值类型">
            <el-select v-model="itemForm.value_type" class="w-full">
              <el-option
                v-for="(label, value) in DictValueTypeMeta"
                :key="value"
                :label="label"
                :value="value"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="排序">
            <el-input-number
              v-model="itemForm.sort"
              class="w-full"
              :min="0"
              :max="100000"
              controls-position="right"
            />
          </el-form-item>
        </div>
        <el-form-item label="值" prop="value">
          <el-input
            v-model="itemForm.value"
            type="textarea"
            :rows="3"
            maxlength="1024"
            show-word-limit
            :placeholder="itemForm.value_type === 'json' ? 'JSON 内容，如 [&quot;a&quot;,&quot;b&quot;]' : '留空则消费侧回落为 Key'"
          />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="itemForm.status" class="w-full">
            <el-option label="启用（消费侧可见）" :value="1" />
            <el-option label="停用（仅管理端可见）" :value="0" />
          </el-select>
        </el-form-item>
        <el-form-item label="备注" prop="remark">
          <el-input v-model="itemForm.remark" maxlength="255" show-word-limit placeholder="可选" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="itemDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="itemSubmitting" @click="submitItem">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
