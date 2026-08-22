import { defineStore } from 'pinia'
import { ref } from 'vue'

/**
 * 多标签工作区（worktabs）store。
 * 标签列表持久化到 localStorage，active 路由由布局组件 watch 驱动后写入。
 */

export interface WorkTab {
  path: string
  title: string
  closable: boolean
}

const TABS_KEY = 'gw_admin_tabs'

const readStoredTabs = (): WorkTab[] => {
  const raw = localStorage.getItem(TABS_KEY)
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw) as WorkTab[]
    return Array.isArray(parsed) ? parsed.filter((t) => t && typeof t.path === 'string') : []
  } catch {
    return []
  }
}

export const useTabsStore = defineStore('tabs', () => {
  const tabs = ref<WorkTab[]>(readStoredTabs())
  const dashboardTab: WorkTab = { path: '/dashboard', title: '仪表盘', closable: false }

  const persist = () => {
    localStorage.setItem(TABS_KEY, JSON.stringify(tabs.value))
  }

  /** 确保仪表盘常驻首标签。 */
  const ensureDashboard = () => {
    if (!tabs.value.some((t) => t.path === dashboardTab.path)) {
      tabs.value.unshift({ ...dashboardTab })
      persist()
    }
  }

  /** 添加标签（已存在则跳过）。 */
  const upsert = (tab: WorkTab) => {
    if (tabs.value.some((t) => t.path === tab.path)) return
    tabs.value.push(tab)
    persist()
  }

  /** 关闭标签，返回被关闭的 path（调用方据此决定跳转）。 */
  const remove = (path: string): string => {
    tabs.value = tabs.value.filter((t) => t.path !== path)
    persist()
    return path
  }

  /** 关闭其他标签，保留仪表盘与当前标签。 */
  const closeOthers = (activePath: string) => {
    tabs.value = tabs.value.filter((t) => t.path === dashboardTab.path || t.path === activePath)
    persist()
  }

  /** 关闭全部，仅保留仪表盘。 */
  const closeAll = () => {
    tabs.value = [{ ...dashboardTab }]
    persist()
  }

  return {
    tabs,
    ensureDashboard,
    upsert,
    remove,
    closeOthers,
    closeAll,
  }
})
