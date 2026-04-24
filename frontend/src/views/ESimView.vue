<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useSimsStore } from '@/stores/sims'
import { useModemsStore } from '@/stores/modems'
import type { SimRow, ModemRow } from '@/types/api'

const simsStore = useSimsStore()
const modemsStore = useModemsStore()
const loading = ref(true)

const esimSims = computed(() =>
  simsStore.sims.filter(
    (s) => s.card_type === 'sticker_esim' || s.card_type === 'embedded_esim',
  ),
)

function findModem(sim: SimRow): ModemRow | undefined {
  return modemsStore.modems.find((m) => m.sim?.id === sim.id)
}

function cardTypeLabel(type: string): string {
  switch (type) {
    case 'sticker_esim':
      return 'eSIM (贴片)'
    case 'embedded_esim':
      return 'eSIM (内嵌)'
    default:
      return type
  }
}

onMounted(async () => {
  await Promise.all([simsStore.fetchSims(), modemsStore.fetchModems()])
  loading.value = false
})
</script>

<template>
  <div class="page-container">
    <h2 style="margin-bottom: 20px">eSIM 管理</h2>

    <el-alert
      type="info"
      title="Profile 切换功能将在后续版本中启用"
      description="需要 lpac 集成，当前版本仅展示已识别的 eSIM 信息。"
      show-icon
      :closable="false"
      style="margin-bottom: 20px"
    />

    <div v-loading="loading">
      <el-row :gutter="16">
        <el-col
          v-for="sim in esimSims"
          :key="sim.id"
          :xs="24"
          :sm="12"
          :md="8"
          style="margin-bottom: 16px"
        >
          <el-card shadow="hover" class="esim-card">
            <div class="esim-card__header">
              <el-tag type="success" size="small" effect="dark">
                {{ cardTypeLabel(sim.card_type) }}
              </el-tag>
              <el-tag v-if="sim.esim_profile_active" type="success" size="small" effect="plain">
                活跃
              </el-tag>
            </div>

            <div class="esim-card__info">
              <div class="esim-card__row">
                <span class="esim-card__label">ICCID</span>
                <span class="mono">{{ sim.iccid }}</span>
              </div>
              <div v-if="sim.imsi" class="esim-card__row">
                <span class="esim-card__label">IMSI</span>
                <span class="mono">{{ sim.imsi }}</span>
              </div>
              <div v-if="sim.operator_name" class="esim-card__row">
                <span class="esim-card__label">运营商</span>
                <span>{{ sim.operator_name }}</span>
              </div>
              <div v-if="sim.esim_profile_nickname" class="esim-card__row">
                <span class="esim-card__label">昵称</span>
                <span>{{ sim.esim_profile_nickname }}</span>
              </div>
              <div v-if="findModem(sim)" class="esim-card__row">
                <span class="esim-card__label">模块</span>
                <span>{{ findModem(sim)?.manufacturer }} {{ findModem(sim)?.model }}</span>
              </div>
            </div>

            <!-- Profiles 面板占位 -->
            <el-divider />
            <div class="esim-card__profiles">
              <span class="esim-card__label">Profiles</span>
              <el-tag size="small" type="info">待 lpac 集成</el-tag>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-empty
        v-if="!loading && esimSims.length === 0"
        description="未检测到 eSIM"
        :image-size="100"
      >
        <template #description>
          <p>未检测到 eSIM 卡</p>
          <p style="font-size: 12px; color: var(--el-text-color-secondary)">
            系统会自动识别贴片 eSIM (9eSIM) 和嵌入式 eSIM
          </p>
        </template>
      </el-empty>
    </div>
  </div>
</template>

<style scoped lang="scss">
.esim-card {
  border-radius: 12px;

  &__header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 16px;
  }

  &__info {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  &__row {
    display: flex;
    gap: 8px;
    align-items: center;
    font-size: 13px;
  }

  &__label {
    color: var(--el-text-color-secondary);
    min-width: 50px;
    font-size: 13px;
  }

  &__profiles {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }
}

.mono {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
}
</style>
