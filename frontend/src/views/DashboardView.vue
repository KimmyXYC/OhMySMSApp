<script setup lang="ts">
import { onMounted } from 'vue'
import { useModemsStore } from '@/stores/modems'
import { useSimsStore } from '@/stores/sims'
import ModemCard from '@/components/ModemCard.vue'
import { Refresh } from '@element-plus/icons-vue'

const modemsStore = useModemsStore()
const simsStore = useSimsStore()

onMounted(async () => {
  await Promise.all([modemsStore.fetchModems(), simsStore.fetchSims()])
})

function handleRefresh() {
  modemsStore.fetchModems()
  simsStore.fetchSims()
}
</script>

<template>
  <div class="page-container">
    <div class="dashboard-header">
      <h2>控制面板</h2>
      <el-button :icon="Refresh" circle @click="handleRefresh" :loading="modemsStore.loading" />
    </div>

    <!-- 统计卡片 -->
    <el-row :gutter="16" class="dashboard-stats">
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover">
          <el-statistic title="模块总数" :value="modemsStore.modems.length" />
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover">
          <el-statistic title="在线" :value="modemsStore.onlineModems.length" />
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover">
          <el-statistic title="离线" :value="modemsStore.offlineModems.length" />
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover">
          <el-statistic title="SIM 卡" :value="simsStore.sims.length" />
        </el-card>
      </el-col>
    </el-row>

    <!-- 模块卡片网格 -->
    <h3 style="margin: 24px 0 16px">模块列表</h3>

    <el-alert
      v-if="modemsStore.error"
      type="error"
      :title="modemsStore.error"
      show-icon
      closable
      style="margin-bottom: 16px"
    />

    <el-skeleton :loading="modemsStore.loading && modemsStore.modems.length === 0" :rows="4" animated>
      <template #default>
        <el-row :gutter="16">
          <el-col
            v-for="modem in modemsStore.modems"
            :key="modem.id"
            :xs="24"
            :sm="12"
            :md="8"
            :lg="6"
            style="margin-bottom: 16px"
          >
            <ModemCard :modem="modem" />
          </el-col>
        </el-row>

        <el-empty
          v-if="modemsStore.modems.length === 0"
          description="暂无模块，请检查 USB 连接"
        />
      </template>
    </el-skeleton>
  </div>
</template>

<style scoped lang="scss">
.dashboard-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;

  h2 {
    font-size: 22px;
    font-weight: 600;
  }
}

.dashboard-stats {
  margin-bottom: 8px;

  .el-card {
    border-radius: 12px;
  }
}
</style>
