<script setup lang="ts">
import { onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useModemsStore } from '@/stores/modems'
import { useSimsStore } from '@/stores/sims'
import ModemCard from '@/components/ModemCard.vue'
import { Refresh } from '@element-plus/icons-vue'
import type { ModemRow } from '@/types/api'

const modemsStore = useModemsStore()
const simsStore = useSimsStore()

const activeSims = computed(() => simsStore.sims.length)

onMounted(async () => {
  await Promise.all([modemsStore.fetchModems(), simsStore.fetchSims()])
})

function handleRefresh() {
  modemsStore.fetchModems()
  simsStore.fetchSims()
}

async function handleDeleteModem(modem: ModemRow) {
  if (modem.present) {
    ElMessage.warning('在线模块不能删除')
    return
  }
  const name = modem.nickname || modem.model || modem.device_id
  try {
    await ElMessageBox.confirm(
      `确定删除离线模块「${name}」？短信记录会保留，但该模块的信号历史会一起删除。`,
      '删除离线模块',
      {
        confirmButtonText: '删除',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--danger',
      },
    )
  } catch {
    return
  }
  try {
    await modemsStore.doDeleteModem(modem.device_id)
    ElMessage.success('模块已删除')
  } catch (e: any) {
    const code = e.response?.data?.code
    if (code === 'modem_in_use') {
      ElMessage.error('模块当前在线，不能删除')
    } else {
      ElMessage.error(e.response?.data?.error || e.message || '删除模块失败')
    }
  }
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
        <el-card shadow="hover" class="stat-card">
          <div class="stat-card__accent" />
          <el-statistic title="模块总数" :value="modemsStore.modems.length" />
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card stat-card--online">
          <div class="stat-card__accent stat-card__accent--online" />
          <div class="stat-with-dot">
            <span class="stat-dot stat-dot--online" />
            <el-statistic title="在线" :value="modemsStore.onlineModems.length" />
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-card__accent stat-card__accent--info" />
          <el-statistic title="已插卡" :value="modemsStore.modemsWithSim.length" />
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-card__accent stat-card__accent--success" />
          <el-statistic title="SIM 卡" :value="activeSims" />
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

    <div v-loading="modemsStore.loading && modemsStore.modems.length === 0">
      <el-row :gutter="16">
        <el-col
          v-for="modem in modemsStore.modems"
          :key="modem.device_id"
          :xs="24"
          :sm="12"
          :md="8"
          :lg="6"
          style="margin-bottom: 16px"
        >
          <ModemCard :modem="modem" @delete="handleDeleteModem" />
        </el-col>
      </el-row>

      <el-empty
        v-if="!modemsStore.loading && modemsStore.modems.length === 0"
        description="暂无模块，请检查 USB 连接"
      />
    </div>
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

@media (max-width: 767px) {
  .dashboard-header {
    align-items: flex-start;
    gap: 12px;
    flex-direction: column;
  }

  .dashboard-stats :deep(.el-statistic__content) {
    font-size: 20px;
  }
}

.dashboard-stats {
  margin-bottom: 8px;
}

.stat-card {
  border-radius: 12px;
  position: relative;
  overflow: hidden;

  &__accent {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 3px;
    background: var(--ohmysms-primary);
    border-radius: 12px 12px 0 0;

    &--online {
      background: var(--ohmysms-primary);
    }

    &--info {
      background: var(--ohmysms-info);
    }

    &--success {
      background: var(--ohmysms-success);
    }
  }
}

.stat-with-dot {
  position: relative;
}

.stat-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  position: absolute;
  top: 4px;
  right: 4px;

  &--online {
    background-color: var(--ohmysms-online-dot);
    box-shadow: 0 0 6px var(--ohmysms-online-glow);
    animation: pulse-dot 2s infinite;
  }
}

@keyframes pulse-dot {
  0%,
  100% {
    box-shadow: 0 0 6px var(--ohmysms-online-glow);
  }
  50% {
    box-shadow: 0 0 12px var(--ohmysms-online-glow);
  }
}
</style>
