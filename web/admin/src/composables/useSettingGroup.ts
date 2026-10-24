import { ElMessage } from 'element-plus'
import { ref, type Ref } from 'vue'
import { dictApi } from '@/api/dict'
import { ApiError } from '@/api/http'
import { BusinessCode } from '@/types/api'
import { notifyError } from '@/utils/feedback'

/**
 * 以「整组 setting」的方式读写一个字典分组。
 *
 * - 读取：GET /dicts/:group（仅启用项），按 key 映射到表单默认值；
 * - 保存：PUT /dicts/:group，object → items（整组替换）；
 * - 分组尚不存在时按默认值渲染，首次保存自动创建。
 */
export function useSettingGroup<T extends Record<string, string>>(group: string, defaults: T) {
  const data = ref<T>({ ...defaults }) as Ref<T>
  const loading = ref(false)
  const saving = ref(false)
  const exists = ref(false)

  const reset = () => {
    data.value = { ...defaults } as T
  }

  const load = async () => {
    loading.value = true
    try {
      const items = await dictApi.values(group)
      const stored: Record<string, string> = {}
      for (const item of items) {
        stored[item.key] = item.value
      }
      data.value = { ...defaults, ...stored } as T
      exists.value = true
    } catch (error) {
      if (error instanceof ApiError && error.code === BusinessCode.NOT_FOUND) {
        reset()
        exists.value = false
        return
      }
      notifyError(error, '加载设置失败')
    } finally {
      loading.value = false
    }
  }

  const save = async () => {
    saving.value = true
    try {
      // 空字段不落库：整组替换时省略即删除，避免字典里堆积空 item。
      const payload = Object.fromEntries(
        Object.entries(data.value).filter(([, value]) => value !== ''),
      )
      await dictApi.replace(group, payload)
      exists.value = true
      ElMessage.success('保存成功')
      return true
    } catch (error) {
      notifyError(error, '保存设置失败')
      return false
    } finally {
      saving.value = false
    }
  }

  return { data, loading, saving, exists, load, reset, save }
}
