<script setup lang="ts">
import { ref, onMounted, computed, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useModemsStore } from '@/stores/modems'
import { getSignalHistory } from '@/api/signal'
import { resetModem } from '@/api/modems'
import SignalBars from '@/components/SignalBars.vue'
import SimBadge from '@/components/SimBadge.vue'
import { ElMessage } from 'element-plus'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart } from 'echarts/charts'
import {
  GridComponent,
  TooltipComponent,
  LegendComponent,
  DataZoomComponent,
} from 'echarts/components'
import VChart from 'vue-echarts'
import type { ModemRow, SignalRow } from '@/types/api'

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, LegendComponent, DataZoomComponent])

const route = useRoute()
const router = useRouter()
const modemsStore = useModemsStore()

const modem = ref<ModemRow | null>(null)
const loading = ref(true)
const signalHistory = ref<SignalRow[]>([])
const chartLoading = ref(false)

const deviceId = computed(() => route.params.deviceId as string)

// 信号图表选项
const chartOption = computed(() => {
  const data = signalHistory.value
  if (!data.length) return null

  const times = data.map((s) => new Date(s.sampled_at).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }))
  const qualities = data.map((s) => s.quality_pct ?? 0)

  return {
    tooltip: {
      trigger: 'axis' as const,
      formatter: (params: any) => {
        const p = Array.isArray(params) ? params[0] : params
        return `${p.name}<br/>信号: ${p.value}%`
      },
    },
    grid: {
      left: 50,
      right: 20,
      top: 20,
      bottom: 40,
    },
    xAxis: {
      type: 'category' as const,
      data: times,
      axisLabel: { fontSize: 11 },
    },
    yAxis: {
      type: 'value' as const,
      min: 0,
      max: 100,
      axisLabel: {
        formatter: '{value}%',
        fontSize: 11,
      },
    },
    series: [
      {
        name: '信号质量',
        type: 'line' as const,
        data: qualities,
        smooth: true,
        showSymbol: false,
        lineStyle: { width: 2 },
        areaStyle: {
          opacity: 0.15,
        },
        itemStyle: {
          color: '#60a5fa',
        },
      },
    ],
  }
})

async function loadSignalHistory() {
  if (!deviceId.value) return
  chartLoading.value = true
  try {
    const { data } = await getSignalHistory(deviceId.value, 120)
    // 后端返回 DESC，图表需要 ASC
    signalHistory.value = (data.items ?? []).reverse()
  } catch {
    // silently ignore
  } finally {
    chartLoading.value = false
  }
}

// 自动刷新信号
let refreshInterval: ReturnType<typeof setInterval> | undefined

onMounted(async () => {
  try {
    modem.value = await modemsStore.fetchModem(deviceId.value)
  } catch (e: any) {
    ElMessage.error('模块未找到')
    router.push({ name: 'dashboard' })
    return
  } finally {
    loading.value = false
  }

  await loadSignalHistory()

  // 每 30 秒刷新信号图表
  refreshInterval = setInterval(() => {
    loadSignalHistory()
  }, 30000)
})

onUnmounted(() => {
  if (refreshInterval) clearInterval(refreshInterval)
})

function handleReset() {
  ElMessage.info('功能计划中，后端尚未实现')
}

// 获取端口列表
const portList = computed(() => {
  if (!modem.value) return []
  const ports: string[] = []
  if (modem.value.primary_port) ports.push(`主端口: ${modem.value.primary_port}`)
  if (modem.value.at_ports?.length) ports.push(`AT: ${modem.value.at_ports.join(', ')}`)
  if (modem.value.qmi_port) ports.push(`QMI: ${modem.value.qmi_port}`)
  if (modem.value.mbim_port) ports.push(`MBIM: ${modem.value.mbim_port}`)
  return ports
})
</script>

<template>
  <div class="page-container">
    <el-page-header @back="router.back()" title="返回">
      <template #content>
        <span v-if="modem">{{ modem.manufacturer }} {{ modem.model }} — {{ modem.device_id?.slice(-8) }}</span>
      </template>
      <template #extra>
        <el-tooltip content="功能计划中，后端尚未实现" placement="bottom">
          <el-button type="warning" disabled @click="handleReset">
            重置模块
          </el-button>
        </el-tooltip>
      </template>
    </el-page-header>

    <div v-loading="loading">
      <div v-if="modem" style="margin-top: 24px">
        <!-- 基本信息 -->
        <el-descriptions :column="2" border>
          <el-descriptions-item label="Device ID">
            <span class="mono">{{ modem.device_id }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="IMEI">
            <span class="mono">{{ modem.imei ?? '—' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="制造商">{{ modem.manufacturer ?? '—' }}</el-descriptions-item>
          <el-descriptions-item label="型号">{{ modem.model ?? '—' }}</el-descriptions-item>
          <el-descriptions-item label="固件">{{ modem.firmware ?? '—' }}</el-descriptions-item>
          <el-descriptions-item label="USB 路径">{{ modem.usb_path ?? '—' }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="modem.present ? 'success' : 'danger'">
              {{ modem.present ? '在线' : '离线' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="信号">
            <SignalBars :quality="modem.signal?.quality_pct ?? null" />
          </el-descriptions-item>
        </el-descriptions>

        <!-- 信号历史图表 -->
        <h3 style="margin: 24px 0 12px">信号历史</h3>
        <el-card shadow="never" class="chart-card" v-loading="chartLoading">
          <VChart
            v-if="chartOption"
            :option="chartOption"
            :autoresize="true"
            style="height: 260px; width: 100%"
          />
          <el-empty
            v-else-if="!chartLoading"
            description="暂无信号数据"
            :image-size="60"
          />
        </el-card>

        <!-- 端口列表 -->
        <h3 style="margin: 24px 0 12px">端口</h3>
        <el-card shadow="never" class="info-card">
          <div v-if="portList.length > 0" class="port-list">
            <el-tag
              v-for="(port, idx) in portList"
              :key="idx"
              type="info"
              effect="plain"
              size="default"
              style="margin: 4px"
            >
              {{ port }}
            </el-tag>
          </div>
          <span v-else style="color: var(--el-text-color-secondary)">无端口信息</span>
        </el-card>

        <!-- SIM 卡 -->
        <h3 style="margin: 24px 0 12px">SIM 卡</h3>
        <SimBadge v-if="modem.sim" :sim="modem.sim" />
        <el-empty v-else description="未检测到 SIM 卡" :image-size="60" />
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.chart-card,
.info-card {
  border-radius: 12px;
}

.port-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.mono {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
}
</style>
