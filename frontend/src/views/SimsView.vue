<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useSimsStore } from '@/stores/sims'
import { useModemsStore } from '@/stores/modems'
import type { SimRow, ModemRow } from '@/types/api'

const router = useRouter()
const simsStore = useSimsStore()
const modemsStore = useModemsStore()

const viewMode = ref<'table' | 'card'>('card')
const loading = computed(() => simsStore.loading)
const isMobile = ref(false)

const msisdnDialogVisible = ref(false)
const msisdnDialogSaving = ref(false)
const msisdnEditingSim = ref<SimRow | null>(null)
const msisdnInput = ref('')

const msisdnDialogTitle = computed(() => {
  const sim = msisdnEditingSim.value
  if (!sim) return '编辑号码'
  return hasMsisdnOverride(sim) ? '编辑本地显示号码' : '设置本地显示号码'
})

const handleResize = () => {
  isMobile.value = window.innerWidth <= 767
  if (isMobile.value) viewMode.value = 'card'
}

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

function hasMsisdnOverride(sim: SimRow): boolean {
  return !!sim.msisdn_override
}

function openMsisdnDialog(sim: SimRow) {
  msisdnEditingSim.value = sim
  // 只在已有本地覆盖时回填覆盖值；自动获取号码仅作为提示，避免误保存成覆盖。
  msisdnInput.value = sim.msisdn_override ?? ''
  msisdnDialogVisible.value = true
}

function closeMsisdnDialog() {
  if (msisdnDialogSaving.value) return
  msisdnDialogVisible.value = false
}

function resetMsisdnDialog() {
  msisdnEditingSim.value = null
  msisdnInput.value = ''
}

async function refreshAfterMsisdnChange() {
  await Promise.all([simsStore.fetchSims(), modemsStore.fetchModems()])

  const errors = [simsStore.error, modemsStore.error].filter(Boolean)
  if (errors.length > 0) {
    throw new Error(errors.join('；'))
  }
}

async function saveMsisdnOverride() {
  const sim = msisdnEditingSim.value
  if (!sim) return

  const value = msisdnInput.value.trim()
  msisdnDialogSaving.value = true
  try {
    await simsStore.updateSimMsisdn(sim.id, value)
    ElMessage.success(value ? '本地显示号码已保存' : '已清除号码覆盖，将回退自动获取值')
    msisdnDialogVisible.value = false

    try {
      await refreshAfterMsisdnChange()
    } catch (refreshError: any) {
      ElMessage.warning(refreshError.message || '号码已保存，但刷新 SIM/模块列表失败')
    }
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '保存号码失败')
  } finally {
    msisdnDialogSaving.value = false
  }
}

function goToSms(sim: SimRow) {
  // 跳转到 SMS 并以 sim 的 iccid 关联的 device 做过滤
  const modem = findModem(sim)
  if (modem) {
    router.push({ name: 'sms', query: { device_id: modem.device_id } })
  } else {
    router.push({ name: 'sms', query: { sim_id: String(sim.id) } })
  }
}

function goToModem(modem: ModemRow) {
  router.push({ name: 'modem-detail', params: { deviceId: modem.device_id } })
}

function isSimInUse(sim: SimRow): boolean {
  return !!findModem(sim)
}

async function handleDeleteSim(sim: SimRow) {
  if (isSimInUse(sim)) {
    ElMessage.warning('正在使用的 SIM 卡不能删除')
    return
  }
  try {
    await ElMessageBox.confirm(
      `确定删除未使用的 SIM「${sim.iccid}」？短信记录会保留，但会解除与该 SIM 的关联。`,
      '删除 SIM 卡',
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
    await simsStore.doDeleteSim(sim.id)
    ElMessage.success('SIM 卡已删除')
  } catch (e: any) {
    const code = e.response?.data?.code
    if (code === 'sim_in_use') {
      ElMessage.error('该 SIM 当前仍绑定在模块上，不能删除')
      await Promise.all([simsStore.fetchSims(), modemsStore.fetchModems()])
    } else {
      ElMessage.error(e.response?.data?.error || e.message || '删除 SIM 失败')
    }
  }
}

onMounted(async () => {
  handleResize()
  window.addEventListener('resize', handleResize)
  await Promise.all([simsStore.fetchSims(), modemsStore.fetchModems()])
})

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
})
</script>

<template>
  <div class="page-container">
    <div class="sims-header">
      <h2>SIM 卡</h2>
      <el-radio-group v-if="!isMobile" v-model="viewMode" size="small">
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
                <span class="sim-card__number">
                  <span>{{ sim.msisdn ?? '未知' }}</span>
                  <el-tag v-if="hasMsisdnOverride(sim)" size="small" type="info" effect="plain">
                    本地覆盖
                  </el-tag>
                </span>
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
              <el-button size="small" type="primary" text @click="openMsisdnDialog(sim)">
                编辑号码
              </el-button>
              <el-button size="small" type="primary" text @click="goToSms(sim)">
                查看短信
              </el-button>
              <el-button
                size="small"
                type="danger"
                text
                :disabled="isSimInUse(sim)"
                @click="handleDeleteSim(sim)"
              >
                删除
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
            <div class="sim-number-cell">
              <span>{{ row.msisdn ?? '—' }}</span>
              <el-tag v-if="hasMsisdnOverride(row)" size="small" type="info" effect="plain">
                本地
              </el-tag>
            </div>
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
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" text type="primary" @click="openMsisdnDialog(row)">
              号码
            </el-button>
            <el-button size="small" text type="primary" @click="goToSms(row)">
              短信
            </el-button>
            <el-button
              size="small"
              text
              type="danger"
              :disabled="isSimInUse(row)"
              @click="handleDeleteSim(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty
        v-if="!loading && simsStore.sims.length === 0"
        description="暂无 SIM 卡"
      />
    </div>

    <el-dialog
      v-model="msisdnDialogVisible"
      :title="msisdnDialogTitle"
      width="min(520px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      @closed="resetMsisdnDialog"
    >
      <el-alert
        title="这是 ohmysmsapp 的本地显示号码，仅用于本系统页面展示；不会写入 SIM 卡，也不会修改运营商资料。"
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 16px"
      />

      <el-form label-position="top">
        <el-form-item label="本地显示号码">
          <el-input
            v-model="msisdnInput"
            placeholder="输入本地显示号码；留空保存即清除本地覆盖"
            clearable
            maxlength="32"
            @keyup.enter="saveMsisdnOverride"
          />
        </el-form-item>
      </el-form>

      <div class="msisdn-dialog__hint">
        <p>保存非空内容后，该号码会优先作为 SIM 的展示号码。</p>
        <p v-if="msisdnEditingSim?.msisdn && !hasMsisdnOverride(msisdnEditingSim)">
          自动获取号码：<span class="mono">{{ msisdnEditingSim.msisdn }}</span>（仅作参考，不会自动保存为本地覆盖）
        </p>
        <p>留空并保存表示清除本地覆盖，并回退到后端自动获取的号码（如有）。</p>
        <p v-if="msisdnEditingSim" class="mono">
          ICCID: {{ msisdnEditingSim.iccid }}
        </p>
      </div>

      <template #footer>
        <div class="msisdn-dialog__footer">
          <el-button @click="closeMsisdnDialog">取消</el-button>
          <el-button type="primary" :loading="msisdnDialogSaving" @click="saveMsisdnOverride">
            保存
          </el-button>
        </div>
      </template>
    </el-dialog>
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
    min-width: 0;
  }

  &__number {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex-wrap: wrap;
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
    display: flex;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 4px 8px;
    margin-top: 12px;
    text-align: right;

    .el-button {
      margin-left: 0;
    }
  }
}

.sim-number-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.msisdn-dialog__hint {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;

  p {
    margin: 4px 0;
  }
}

.msisdn-dialog__footer {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;

  .el-button {
    margin-left: 0;
  }
}

.mono {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
}

@media (max-width: 767px) {
  .sims-header {
    align-items: flex-start;
    gap: 12px;
    flex-direction: column;
  }

  .sim-card__row {
    align-items: flex-start;
  }

  .sim-card__row span:last-child,
  .mono {
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .sim-card__actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
    text-align: initial;

    .el-button {
      width: 100%;
      margin-left: 0;
    }
  }

  .msisdn-dialog__footer {
    display: grid;
    grid-template-columns: 1fr;

    .el-button {
      width: 100%;
    }
  }
}
</style>
