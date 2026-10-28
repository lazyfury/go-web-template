<script setup lang="ts">
import type { UploadFile } from 'element-plus'
import { computed, onMounted, ref } from 'vue'
import { fileApi } from '@/api/file'
import { usePagination } from '@/composables/usePagination'
import { useAuthStore } from '@/stores/auth'
import {
  DefaultAllowedExt,
  DefaultMaxUploadBytes,
  FileBizTypeOptions,
  FileVisibilityMeta,
  fileExt,
  formatFileSize,
  type FileItem,
  type FileVisibility,
} from '@/types/file'
import { notifyError } from '@/utils/feedback'

/**
 * 文件管理：上传 / 列表 / 签名下载 / 可见性 / 删除。
 *
 * 字节不经过本页面：上传是 multipart 直传 API，下载先换签名 URL（本地驱动由
 * API 自身校验 HMAC，S3 驱动是预签名 URL），浏览器直接拉取。
 */

const auth = useAuthStore()
const canWrite = computed(() => auth.can('files:write'))

const { page, pageSize, total, loading, reset, onPageChange, onPageSizeChange } = usePagination()
const items = ref<FileItem[]>([])
const keyword = ref('')
const bizType = ref('')

const uploadVisible = ref(false)
const selectedFile = ref<File | null>(null)
const uploadBizType = ref('attachment')
const uploadVisibility = ref<FileVisibility>(0)
const uploading = ref(false)
const progress = ref(0)

const load = async () => {
  loading.value = true
  try {
    const result = await fileApi.list({
      page: page.value,
      page_size: pageSize.value,
      keyword: keyword.value || undefined,
      biz_type: bizType.value || undefined,
    })
    items.value = result.items
    total.value = result.total
  } catch (error) {
    notifyError(error, '加载文件列表失败')
  } finally {
    loading.value = false
  }
}

const search = async () => {
  reset()
  await load()
}

const openUpload = () => {
  selectedFile.value = null
  uploadBizType.value = 'attachment'
  uploadVisibility.value = 0
  progress.value = 0
  uploadVisible.value = true
}

/** 前端只做体验层校验；大小/类型/内容的最终判定在服务端。 */
const onFileChange = (file: UploadFile) => {
  const raw = file.raw
  if (!raw) return
  if (raw.size > DefaultMaxUploadBytes) {
    ElMessage.error(`文件不能超过 ${formatFileSize(DefaultMaxUploadBytes)}`)
    selectedFile.value = null
    return
  }
  if (!DefaultAllowedExt.includes(fileExt(raw.name))) {
    ElMessage.error('该文件类型不在允许列表中')
    selectedFile.value = null
    return
  }
  selectedFile.value = raw
}

const submitUpload = async () => {
  if (!selectedFile.value) {
    ElMessage.warning('请先选择文件')
    return
  }
  uploading.value = true
  progress.value = 0
  try {
    await fileApi.upload(selectedFile.value, {
      bizType: uploadBizType.value || undefined,
      visibility: uploadVisibility.value,
      onProgress: (value) => {
        progress.value = value
      },
    })
    ElMessage.success('上传成功')
    uploadVisible.value = false
    await load()
  } catch (error) {
    notifyError(error, '上传失败')
  } finally {
    uploading.value = false
  }
}

const download = async (row: FileItem, disposition: 'inline' | 'attachment') => {
  try {
    const result = await fileApi.url(row.id, disposition)
    window.open(result.url, '_blank', 'noopener')
  } catch (error) {
    notifyError(error, '获取下载地址失败')
  }
}

/**
 * 长期公开链接（文章封面/头像用）：后端 file_shares 生成随机 token，
 * 同 (file, purpose) 幂等、可撤销；链接本身可被 CDN/浏览器缓存。
 */
const copyPublicLink = async (row: FileItem) => {
  try {
    const link = await fileApi.share(row.id, { purpose: 'asset' })
    await navigator.clipboard.writeText(link.url)
    ElMessage.success('已复制长期外链（可撤销，缓存生效期间仍可能被命中）')
  } catch (error) {
    notifyError(error, '生成外链失败')
  }
}

const revokePublicLink = async (row: FileItem) => {
  try {
    await ElMessageBox.confirm(`确定撤销「${row.name}」的所有公开外链？已缓存的页面最多在缓存过期后才失效。`, '撤销外链', {
      type: 'warning',
      confirmButtonText: '撤销',
      cancelButtonText: '取消',
    })
    await fileApi.revokeShare(row.id)
    ElMessage.success('已撤销公开外链')
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    notifyError(error, '撤销外链失败')
  }
}

/** 短签名链接：私有文件临时查看/下载用（TTL 上限 1h），不具备撤销能力。 */
const copyLink = async (row: FileItem) => {
  try {
    // inline 让浏览器直接渲染可预览类型；后端对不在 inline_mime 里的类型会强制
    // 降级为 attachment，所以这里不需要前端再维护一份白名单。
    const result = await fileApi.url(row.id, 'inline')
    await navigator.clipboard.writeText(result.url)
    ElMessage.success('已复制临时下载链接（10 分钟内有效）')
  } catch (error) {
    notifyError(error, '复制链接失败')
  }
}

const toggleVisibility = async (row: FileItem) => {
  const next: FileVisibility = row.visibility === 1 ? 0 : 1
  try {
    await fileApi.updateVisibility(row.id, next)
    ElMessage.success(next === 1 ? '已设为公开（所有登录用户可读）' : '已设为私有')
    await load()
  } catch (error) {
    notifyError(error, '修改可见性失败')
  }
}

const remove = async (row: FileItem) => {
  try {
    await ElMessageBox.confirm(`确定删除「${row.name}」？文件将从存储中清理。`, '删除文件', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
    await fileApi.remove(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    notifyError(error, '删除失败')
  }
}

/* ── 预览 ─────────────────────────────────────────────── */

type PreviewKind = 'image' | 'video' | 'pdf' | 'text' | 'none'

const previewVisible = ref(false)
const previewItem = ref<FileItem | null>(null)
const previewKind = ref<PreviewKind>('none')
const previewURL = ref('')
const previewText = ref('')
const previewTruncated = ref(false)
const previewLoading = ref(false)
const previewError = ref('')

/**
 * 预览策略：
 * - image/video：短签名 URL（inline）直接给 <img>/<video>，不受 X-Frame-Options 影响；
 * - pdf：本地驱动响应带 X-Frame-Options: DENY，iframe 会被拦，改为新标签打开；
 * - text：走后端 JSON 预览（有界 256 KiB），避免 CORS/Content-Disposition 问题；
 * - 其余（zip/docx/xlsx）：不支持内联，给下载入口。
 */
const openPreview = async (row: FileItem) => {
  previewItem.value = row
  previewVisible.value = true
  previewKind.value = 'none'
  previewURL.value = ''
  previewText.value = ''
  previewTruncated.value = false
  previewError.value = ''
  previewLoading.value = true
  try {
    const mime = row.mime_type
    if (mime.startsWith('image/')) {
      previewKind.value = 'image'
      previewURL.value = (await fileApi.url(row.id, 'inline')).url
    } else if (mime.startsWith('video/')) {
      previewKind.value = 'video'
      previewURL.value = (await fileApi.url(row.id, 'inline')).url
    } else if (mime === 'application/pdf') {
      previewKind.value = 'pdf'
      previewURL.value = (await fileApi.url(row.id, 'inline')).url
    } else if (mime.startsWith('text/')) {
      previewKind.value = 'text'
      const preview = await fileApi.previewText(row.id)
      previewText.value = preview.content
      previewTruncated.value = preview.truncated
    } else {
      previewKind.value = 'none'
    }
  } catch (error) {
    previewError.value = '预览加载失败'
    notifyError(error, '预览加载失败')
  } finally {
    previewLoading.value = false
  }
}

const openPreviewInNewTab = () => {
  if (previewURL.value) window.open(previewURL.value, '_blank', 'noopener')
}

const formatTime = (value: string) => new Date(value).toLocaleString()
const accept = DefaultAllowedExt.join(',')

onMounted(load)
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <div class="app-card">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-5 py-4">
        <div>
          <h2 class="text-base font-semibold">文件管理</h2>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">
            上传后按 id 引用；下载通过短期签名 URL，删除后由后台任务清理存储对象。
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <el-input
            v-model="keyword"
            placeholder="搜索文件名"
            clearable
            class="w-44"
            @keyup.enter="search"
            @clear="search"
          />
          <el-select v-model="bizType" clearable placeholder="全部业务类型" class="w-36" @change="search">
            <el-option v-for="option in FileBizTypeOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
          <el-button @click="search"><span class="i-ep-search mr-1" />查询</el-button>
          <el-button v-if="canWrite" type="primary" @click="openUpload">
            <span class="i-ep-upload mr-1" />上传
          </el-button>
        </div>
      </div>

      <el-table v-loading="loading" :data="items" class="px-2">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column label="文件名" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <button
              class="truncate text-left text-[var(--primary)] hover:underline"
              @click="openPreview(row as FileItem)"
            >
              {{ (row as FileItem).name }}
            </button>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="150" show-overflow-tooltip>
          <template #default="{ row }">{{ (row as FileItem).mime_type || '—' }}</template>
        </el-table-column>
        <el-table-column label="大小" width="100">
          <template #default="{ row }">{{ formatFileSize((row as FileItem).size_bytes) }}</template>
        </el-table-column>
        <el-table-column label="业务类型" width="110">
          <template #default="{ row }">
            <span class="text-xs">{{ (row as FileItem).biz_type || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="可见性" width="100">
          <template #default="{ row }">
            <el-tag :type="FileVisibilityMeta[(row as FileItem).visibility].tagType" size="small" effect="light">
              {{ FileVisibilityMeta[(row as FileItem).visibility].label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="上传时间" width="170">
          <template #default="{ row }">{{ formatTime((row as FileItem).created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="480" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openPreview(row as FileItem)">预览</el-button>
            <el-button link type="primary" @click="download(row as FileItem, 'attachment')">下载</el-button>
            <el-button link type="primary" @click="copyPublicLink(row as FileItem)">复制外链</el-button>
            <el-button link type="primary" @click="copyLink(row as FileItem)">临时链接</el-button>
            <el-button link type="warning" @click="revokePublicLink(row as FileItem)">撤销外链</el-button>
            <el-button v-if="canWrite" link type="primary" @click="toggleVisibility(row as FileItem)">
              {{ (row as FileItem).visibility === 1 ? '设为私有' : '设为公开' }}
            </el-button>
            <el-button v-if="canWrite" link type="danger" @click="remove(row as FileItem)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无文件" :image-size="72" />
        </template>
      </el-table>

      <div class="flex justify-end px-5 py-4">
        <el-pagination
          :current-page="page"
          :page-size="pageSize"
          :page-sizes="[10, 20, 50]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @update:current-page="onPageChange"
          @update:page-size="onPageSizeChange"
          @current-change="load"
          @size-change="load"
        />
      </div>
    </div>

    <el-dialog
      v-model="previewVisible"
      :title="previewItem ? `预览 · ${previewItem.name}` : '预览'"
      width="min(880px, 92vw)"
      destroy-on-close
    >
      <div v-loading="previewLoading" class="min-h-32">
        <template v-if="previewKind === 'image'">
          <img :src="previewURL" :alt="previewItem?.name" class="mx-auto max-h-[70vh] max-w-full object-contain" />
        </template>
        <template v-else-if="previewKind === 'video'">
          <video :src="previewURL" controls class="mx-auto max-h-[70vh] w-full bg-black" />
        </template>
        <template v-else-if="previewKind === 'pdf'">
          <div class="flex flex-col items-center gap-3 py-6 text-sm">
            <p>PDF 由浏览器内置阅读器打开（应用响应带 X-Frame-Options: DENY，不能用 iframe 内嵌）。</p>
            <el-button type="primary" @click="openPreviewInNewTab">在新标签打开</el-button>
          </div>
        </template>
        <template v-else-if="previewKind === 'text'">
          <pre class="max-h-[60vh] overflow-auto whitespace-pre-wrap break-all rounded bg-[var(--bg-muted)] p-3 text-xs">{{ previewText }}</pre>
          <p v-if="previewTruncated" class="mt-2 text-xs text-[var(--text-secondary)]">
            文件较大，仅显示前 256 KiB；完整内容请下载。
          </p>
        </template>
        <template v-else>
          <el-empty :description="previewError || '该类型不支持内联预览，请下载查看'" :image-size="72" />
        </template>
      </div>
      <template #footer>
        <el-button v-if="previewItem" @click="download(previewItem, 'attachment')">下载</el-button>
        <el-button @click="previewVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="uploadVisible" title="上传文件" width="480px" :close-on-click-modal="false">
      <el-upload
        drag
        :auto-upload="false"
        :show-file-list="false"
        :accept="accept"
        :on-change="onFileChange"
      >
        <div class="flex flex-col items-center gap-2 py-4">
          <span class="i-ep-upload-filled text-3xl text-[var(--text-muted)]" />
          <p class="text-sm">拖拽文件到此处，或点击选择</p>
          <p class="text-xs text-[var(--text-secondary)]">
            单文件不超过 {{ formatFileSize(DefaultMaxUploadBytes) }}，类型以服务端配置为准
          </p>
        </div>
      </el-upload>
      <p v-if="selectedFile" class="mt-2 text-sm">
        已选择：<span class="font-medium">{{ selectedFile.name }}</span>
        （{{ formatFileSize(selectedFile.size) }}）
      </p>

      <div class="mt-4 flex flex-col gap-3">
        <div class="flex items-center gap-2">
          <span class="w-20 text-sm">业务类型</span>
          <el-select v-model="uploadBizType" class="flex-1">
            <el-option v-for="option in FileBizTypeOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
        </div>
        <div class="flex items-center gap-2">
          <span class="w-20 text-sm">可见性</span>
          <el-radio-group v-model="uploadVisibility">
            <el-radio :value="0">私有</el-radio>
            <el-radio :value="1">公开（登录用户可读）</el-radio>
          </el-radio-group>
        </div>
        <el-progress v-if="uploading" :percentage="progress" :stroke-width="8" />
      </div>

      <template #footer>
        <el-button @click="uploadVisible = false">取消</el-button>
        <el-button type="primary" :loading="uploading" @click="submitUpload">上传</el-button>
      </template>
    </el-dialog>
  </div>
</template>
