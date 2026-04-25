<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { Delete } from '@element-plus/icons-vue'
import type { ModemRow } from '@/types/api'
import { modemLabel } from '@/utils/modemLabel'
import SignalBars from './SignalBars.vue'
import SimBadge from './SimBadge.vue'

const props = defineProps<{
  modem: ModemRow
}>()

const emit = defineEmits<{
  delete: [modem: ModemRow]
}>()

const router = useRouter()

const displayName = computed(() => modemLabel(props.modem))
const hasNickname = computed(() => !!props.modem.nickname?.trim())
const subtitle = computed(() => {
  if (!hasNickname.value) return null
  const parts = [props.modem.manufacturer, props.modem.model].filter(Boolean)
  return parts.join(' · ') || null
})

function goDetail() {
  router.push({ name: 'modem-detail', params: { deviceId: props.modem.device_id } })
}

function handleDelete() {
  emit('delete', props.modem)
}
</script>

<template>
  <el-card shadow="hover" class="modem-card" @click="goDetail" style="cursor: pointer">
    <div class="modem-card__header">
      <div class="modem-card__title">
        <div class="modem-card__name-group">
          <span class="modem-card__name">{{ displayName }}</span>
          <span v-if="subtitle" class="modem-card__subtitle">{{ subtitle }}</span>
        </div>
        <el-tag :type="modem.present ? 'success' : 'danger'" size="small" round>
          {{ modem.present ? '在线' : '离线' }}
        </el-tag>
      </div>
      <SignalBars :quality="modem.signal?.quality_pct ?? null" />
    </div>

    <el-divider style="margin: 12px 0" />

    <div class="modem-card__info">
      <div class="modem-card__row">
        <span class="modem-card__label">IMEI</span>
        <span class="modem-card__value">{{ modem.imei ?? '—' }}</span>
      </div>
      <div class="modem-card__row">
        <span class="modem-card__label">USB</span>
        <span class="modem-card__value">{{ modem.usb_path ?? '—' }}</span>
      </div>
      <div v-if="modem.signal?.access_tech" class="modem-card__row">
        <span class="modem-card__label">网络</span>
        <el-tag size="small" type="info">{{ modem.signal.access_tech.toUpperCase() }}</el-tag>
      </div>
    </div>

    <!-- SIM 信息 -->
    <div v-if="modem.sim" style="margin-top: 12px">
      <SimBadge :sim="modem.sim" compact />
    </div>

    <div v-if="!modem.present" class="modem-card__actions">
      <el-button
        size="small"
        type="danger"
        text
        :icon="Delete"
        @click.stop="handleDelete"
      >
        删除离线模块
      </el-button>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
.modem-card {
  border-radius: 12px;
  transition: transform 0.2s, box-shadow 0.2s;

  &:hover {
    transform: translateY(-2px);
  }

  &__header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
  }

  &__title {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    flex: 1;
  }

  &__name-group {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }

  &__name {
    font-weight: 600;
    font-size: 15px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__subtitle {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    line-height: 1.3;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__info {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  &__row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    min-width: 0;
  }

  &__label {
    color: var(--el-text-color-secondary);
    min-width: 40px;
  }

  &__value {
    font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
    font-size: 12px;
    color: var(--el-text-color-regular);
    min-width: 0;
  }

  &__actions {
    margin-top: 12px;
    text-align: right;
  }
}

@media (max-width: 767px) {
  .modem-card__value {
    overflow-wrap: anywhere;
    word-break: break-word;
    white-space: normal;
  }

  .modem-card__actions .el-button {
    width: 100%;
    min-height: 36px;
    justify-content: center;
  }
}
</style>
