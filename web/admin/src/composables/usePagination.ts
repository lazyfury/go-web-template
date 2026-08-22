import { ref } from 'vue'

/**
 * 列表页分页状态封装。
 * 用法：const { page, pageSize, total, loading, reset, onPageChange, onPageSizeChange } = usePagination()
 * 与 el-pagination 的 update:current-page / update:page-size 事件直接绑定。
 */
export function usePagination(initialPageSize = 20) {
  const page = ref(1)
  const pageSize = ref(initialPageSize)
  const total = ref(0)
  const loading = ref(false)

  /** 搜索/筛选条件变化时回到第一页。 */
  const reset = () => {
    page.value = 1
  }

  const onPageChange = (next: number) => {
    page.value = next
  }

  const onPageSizeChange = (next: number) => {
    pageSize.value = next
    page.value = 1
  }

  return {
    page,
    pageSize,
    total,
    loading,
    reset,
    onPageChange,
    onPageSizeChange,
  }
}
