<script setup lang="ts">
import * as echarts from 'echarts/core'
import { LineChart, PieChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { ECharts } from 'echarts/core'
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import StatCard from '@/components/common/StatCard.vue'
import StatusTag from '@/components/common/StatusTag.vue'
import { userApi } from '@/api/user'
import type { User } from '@/types/user'
import { notifyError } from '@/utils/feedback'

/**
 * 仪表盘。统计数据为演示值（对接真实指标时替换为对应 API），
 * 「最新用户」直接复用 users 列表接口（真实数据）。
 */

const statCards = [
  { title: '今日订单', value: '1,286', trend: '+12.4%', positive: true, iconClass: 'i-ep-sell' },
  { title: '今日销售额', value: '¥ 98,420', trend: '+8.1%', positive: true, iconClass: 'i-ep-coin' },
  { title: '新增用户', value: '324', trend: '+5.7%', positive: true, iconClass: 'i-ep-user' },
  { title: '退款单量', value: '27', trend: '-2.3%', positive: false, iconClass: 'i-ep-refresh-left' },
]

const channels = [
  { name: '自然流量', value: 42, color: '#4f46e5' },
  { name: '广告投放', value: 28, color: '#818cf8' },
  { name: '活动营销', value: 18, color: '#c7d2fe' },
  { name: '老客复购', value: 12, color: '#e0e7ff' },
]

const trendChartRef = ref<HTMLDivElement>()
const donutChartRef = ref<HTMLDivElement>()
let trendChart: ECharts | null = null
let donutChart: ECharts | null = null

const latestUsers = ref<User[]>([])
const latestUsersLoading = ref(false)

const initCharts = () => {
  echarts.use([LineChart, PieChart, GridComponent, TooltipComponent, CanvasRenderer])
  if (trendChartRef.value) {
    trendChart = echarts.init(trendChartRef.value)
    trendChart.setOption({
      tooltip: { trigger: 'axis', backgroundColor: '#fff', borderColor: 'var(--line)', textStyle: { color: '#1f2430', fontSize: 12 } },
      grid: { left: 8, right: 12, top: 24, bottom: 0, containLabel: true },
      xAxis: {
        type: 'category',
        boundaryGap: false,
        data: ['周一', '周二', '周三', '周四', '周五', '周六', '周日'],
        axisLine: { lineStyle: { color: '#e9ebf1' } },
        axisTick: { show: false },
        axisLabel: { color: '#9ca3af' },
      },
      yAxis: {
        type: 'value',
        splitLine: { lineStyle: { color: '#f0f1f5' } },
        axisLabel: { color: '#9ca3af' },
      },
      series: [
        {
          name: '销售额',
          type: 'line',
          smooth: true,
          symbol: 'circle',
          symbolSize: 6,
          data: [12000, 18400, 16200, 20100, 23800, 22400, 26900],
          lineStyle: { width: 3, color: '#4f46e5' },
          itemStyle: { color: '#4f46e5', borderColor: '#fff', borderWidth: 2 },
          areaStyle: {
            color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: 'rgba(79, 70, 229, 0.25)' },
              { offset: 1, color: 'rgba(79, 70, 229, 0.02)' },
            ]),
          },
        },
      ],
    })
  }

  if (donutChartRef.value) {
    donutChart = echarts.init(donutChartRef.value)
    donutChart.setOption({
      tooltip: { trigger: 'item', formatter: '{b}: {c}%' },
      series: [
        {
          type: 'pie',
          radius: ['62%', '82%'],
          center: ['50%', '50%'],
          avoidLabelOverlap: false,
          itemStyle: { borderRadius: 6, borderColor: '#fff', borderWidth: 3 },
          label: { show: false },
          emphasis: { scaleSize: 4 },
          data: channels.map((c) => ({ name: c.name, value: c.value, itemStyle: { color: c.color } })),
        },
      ],
    })
  }
}

const handleResize = () => {
  trendChart?.resize()
  donutChart?.resize()
}

const loadLatestUsers = async () => {
  latestUsersLoading.value = true
  try {
    const result = await userApi.list({ page: 1, page_size: 5 })
    latestUsers.value = result.items
  } catch (error) {
    notifyError(error, '加载最新用户失败')
  } finally {
    latestUsersLoading.value = false
  }
}

const formatDate = (value: string) => value.slice(0, 10)

onMounted(async () => {
  await nextTick()
  initCharts()
  window.addEventListener('resize', handleResize)
  loadLatestUsers()
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
  trendChart?.dispose()
  donutChart?.dispose()
  trendChart = null
  donutChart = null
})
</script>

<template>
  <div class="app-page p-4 md:p-5">
    <!-- 统计卡片 -->
    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <StatCard
        v-for="card in statCards"
        :key="card.title"
        :title="card.title"
        :value="card.value"
        :trend="card.trend"
        :positive="card.positive"
        :icon-class="card.iconClass"
      />
    </div>

    <!-- 图表区 -->
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
      <div class="app-card p-5 lg:col-span-2">
        <div class="mb-2 flex items-center justify-between">
          <div>
            <div class="text-[15px] font-semibold text-[var(--text-primary)]">近 7 日销售趋势</div>
            <div class="mt-0.5 text-12px text-[var(--text-muted)]">演示数据，对接指标接口后替换</div>
          </div>
        </div>
        <div ref="trendChartRef" class="h-72 w-full" />
      </div>

      <div class="app-card p-5">
        <div class="mb-2">
          <div class="text-[15px] font-semibold text-[var(--text-primary)]">渠道占比</div>
          <div class="mt-0.5 text-12px text-[var(--text-muted)]">近 30 天</div>
        </div>
        <div ref="donutChartRef" class="h-44 w-full" />
        <div class="mt-2 space-y-2">
          <div
            v-for="channel in channels"
            :key="channel.name"
            class="flex items-center justify-between text-13px"
          >
            <span class="flex items-center gap-2 text-[var(--text-secondary)]">
              <span class="h-2 w-2 rounded-full" :style="{ backgroundColor: channel.color }" />
              {{ channel.name }}
            </span>
            <span class="num font-medium text-[var(--text-primary)]">{{ channel.value }}%</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 最新用户（真实数据） -->
    <div class="app-card overflow-hidden">
      <div class="flex items-center justify-between px-5 pt-4 pb-1">
        <div class="text-[15px] font-semibold text-[var(--text-primary)]">最新用户</div>
        <router-link to="/users" class="text-13px text-brand-600 hover:underline">
          查看全部 →
        </router-link>
      </div>
      <el-table v-loading="latestUsersLoading" :data="latestUsers" class="mt-1">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="用户名" min-width="140" />
        <el-table-column prop="email" label="邮箱" min-width="220" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <StatusTag :status="row.status" />
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="140">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>
