<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useSimsStore } from '@/stores/sims'
import { useModemsStore } from '@/stores/modems'
import type { SimRow, ModemRow } from '@/types/api'

const router = useRouter()
const simsStore = useSimsStore()
const modemsStore = useModemsStore()

const viewMode = ref<'table' | 'card'>('card')
const loading = computed(() => simsStore.loading)

// 查找 SIM 所属的 Modem
function findModem(sim: SimRow): ModemRow | undefined {
  return modemsStore.modems.find((m) => m.sim?.id === sim.id)
}

function cardTypeLabel(type: string): string {
  switch (type) {
    case 'psim':
      return 'pSIM'
    case 'sticker_esim':
      return 'eSIM (贴片)'
    case 'embedded_esim':
      return 'eSIM (内嵌)'
    default:
      return type
  }
}

function cardTypeTagType(type: string): 'primary' | 'success' | 'warning' {
  switch (type) {
    case 'psim':
      return 'primary'
    case 'sticker_esim':
      return 'success'
    case 'embedded_esim':
      return 'warning'
    default:
      return 'primary'
  }
}

function goToSms(sim: SimRow) {
  // 跳转到 SMS 并以 sim 的 iccid 关联的 device 做过滤
  const modem = findModem(sim)
  if (modem) {
    router.push({ name: 'sms', query: { device_id: modem.device_id } })
  } else {
    router.push({ name: 'sms' })
  }
}

function goToModem(modem: ModemRow) {
  router.push({ name: 'modem-detail', params: { deviceId: modem.device_id } })
}

onMounted(async () => {
  await Promise.all([simsStore.fetchSims(), modemsStore.fetchModems()])
})
</script>

<template>
  <div class="page-container">
    <div class="sims-header">
      <h2>SIM 卡</h2>
      <el-radio-group v-model="viewMode" size="small">
        <el-radio-button value="card">卡片</el-radio-button>
        <el-radio-button value="table">表格</el-radio-button>
      </el-radio-group>
    </div>

    <div v-loading="loading">
      <!-- 卡片视图 -->
      <el-row v-if="viewMode === 'card'" :gutter="16">
        <el-col
          v-for="sim in simsStore.sims"
          :key="sim.id"
          :xs="24"
          :sm="12"
          :md="8"
          :lg="6"
          style="margin-bottom: 16px"
        >
          <el-card shadow="hover" class="sim-card">
            <div class="sim-card__header">
              <el-tag :type="cardTypeTagType(sim.card_type)" size="small" effect="plain">
                {{ cardTypeLabel(sim.card_type) }}
              </el-tag>
              <span v-if="sim.operator_name" class="sim-card__operator">
                {{ sim.operator_name }}
              </span>
            </div>

            <div class="sim-card__info">
              <div class="sim-card__row">
                <span class="sim-card__label">号码</span>
                <span>{{ sim.msisdn ?? '未知' }}</span>
              </div>
              <div class="sim-card__row">
                <span class="sim-card__label">ICCID</span>
                <span class="mono">{{ sim.iccid }}</span>
              </div>
              <div v-if="sim.imsi" class="sim-card__row">
                <span class="sim-card__label">IMSI</span>
                <span class="mono">{{ sim.imsi }}</span>
              </div>
            </div>

            <!-- 所属 Modem -->
            <div v-if="findModem(sim)" class="sim-card__modem">
              <span class="sim-card__label">模块</span>
              <el-link type="primary" @click.stop="goToModem(findModem(sim)!)">
                {{ findModem(sim)?.manufacturer }} {{ findModem(sim)?.model }}
              </el-link>
            </div>

            <div class="sim-card__actions">
              <el-button size="small" type="primary" text @click="goToSms(sim)">
                查看短信
              </el-button>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <!-- 表格视图 -->
      <el-table
        v-if="viewMode === 'table'"
        :data="simsStore.sims"
        stripe
        style="width: 100%"
      >
        <el-table-column prop="iccid" label="ICCID" min-width="200">
          <template #default="{ row }">
            <span class="mono">{{ row.iccid }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="imsi" label="IMSI" min-width="160">
          <template #default="{ row }">
            <span class="mono">{{ row.imsi ?? '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="operator_name" label="运营商" width="120">
          <template #default="{ row }">
            {{ row.operator_name ?? '—' }}
          </template>
        </el-table-column>
        <el-table-column prop="msisdn" label="号码" width="140">
          <template #default="{ row }">
            {{ row.msisdn ?? '—' }}
          </template>
        </el-table-column>
        <el-table-column prop="card_type" label="类型" width="120">
          <template #default="{ row }">
            <el-tag :type="cardTypeTagType(row.card_type)" size="small">
              {{ cardTypeLabel(row.card_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="所属模块" width="180">
          <template #default="{ row }">
            <template v-if="findModem(row)">
              <el-link type="primary" @click="goToModem(findModem(row)!)">
                {{ findModem(row)?.manufacturer }} {{ findModem(row)?.model }}
              </el-link>
            </template>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button size="small" text type="primary" @click="goToSms(row)">
              短信
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty
        v-if="!loading && simsStore.sims.length === 0"
        description="暂无 SIM 卡"
      />
    </div>
  </div>
</template>

<style scoped lang="scss">
.sims-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;

  h2 {
    font-size: 22px;
    font-weight: 600;
  }
}

.sim-card {
  border-radius: 12px;

  &__header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
  }

  &__operator {
    font-weight: 500;
    font-size: 14px;
  }

  &__info {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
  }

  &__row {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  &__label {
    color: var(--el-text-color-secondary);
    min-width: 44px;
    font-size: 13px;
  }

  &__modem {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--el-border-color-extra-light);
    font-size: 13px;
  }

  &__actions {
    margin-top: 12px;
    text-align: right;
  }
}

.mono {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
}
</style>
