<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useModemsStore } from '@/stores/modems'
import SignalBars from '@/components/SignalBars.vue'
import SimBadge from '@/components/SimBadge.vue'
import type { Modem } from '@/types/api'

const route = useRoute()
const modemsStore = useModemsStore()
const modem = ref<Modem | null>(null)
const loading = ref(true)

onMounted(async () => {
  const id = Number(route.params.id)
  try {
    modem.value = await modemsStore.fetchModem(id)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="page-container">
    <el-page-header @back="$router.back()" title="返回">
      <template #content>
        <span v-if="modem">{{ modem.manufacturer }} {{ modem.model }} — {{ modem.imei }}</span>
      </template>
    </el-page-header>

    <el-skeleton :loading="loading" :rows="8" animated>
      <template #default>
        <div v-if="modem" style="margin-top: 24px">
          <el-descriptions :column="2" border>
            <el-descriptions-item label="IMEI">{{ modem.imei ?? '—' }}</el-descriptions-item>
            <el-descriptions-item label="制造商">{{ modem.manufacturer ?? '—' }}</el-descriptions-item>
            <el-descriptions-item label="型号">{{ modem.model ?? '—' }}</el-descriptions-item>
            <el-descriptions-item label="固件">{{ modem.firmware ?? '—' }}</el-descriptions-item>
            <el-descriptions-item label="USB 路径">{{ modem.usb_path ?? '—' }}</el-descriptions-item>
            <el-descriptions-item label="主端口">{{ modem.primary_port ?? '—' }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="modem.present ? 'success' : 'danger'">
                {{ modem.present ? '在线' : '离线' }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="信号">
              <SignalBars :quality="modem.signal?.quality_pct ?? null" />
            </el-descriptions-item>
          </el-descriptions>

          <h3 style="margin: 24px 0 12px">SIM 卡</h3>
          <SimBadge v-if="modem.sim" :sim="modem.sim" />
          <el-empty v-else description="未检测到 SIM 卡" :image-size="60" />

          <!-- TODO 阶段 3：信号历史图表、短信快捷入口、USSD 快捷入口 -->
          <el-divider />
          <el-alert type="info" title="更多功能将在阶段 3 实现" description="信号历史图表、短信收发、USSD 操作等" show-icon :closable="false" />
        </div>
      </template>
    </el-skeleton>
  </div>
</template>
