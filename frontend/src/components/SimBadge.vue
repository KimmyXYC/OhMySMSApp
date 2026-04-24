<script setup lang="ts">
import type { Sim } from '@/types/api'

defineProps<{
  sim: Sim
  compact?: boolean
}>()

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
</script>

<template>
  <div class="sim-badge" :class="{ 'sim-badge--compact': compact }">
    <div class="sim-badge__header">
      <el-tag :type="cardTypeTagType(sim.card_type)" size="small" effect="plain">
        {{ cardTypeLabel(sim.card_type) }}
      </el-tag>
      <span v-if="sim.operator_name" class="sim-badge__operator">{{ sim.operator_name }}</span>
    </div>

    <template v-if="!compact">
      <div class="sim-badge__row">
        <span class="sim-badge__label">号码</span>
        <span class="sim-badge__value">{{ sim.msisdn ?? '未知' }}</span>
      </div>
      <div class="sim-badge__row">
        <span class="sim-badge__label">ICCID</span>
        <span class="sim-badge__value sim-badge__value--mono">{{ sim.iccid }}</span>
      </div>
      <div v-if="sim.imsi" class="sim-badge__row">
        <span class="sim-badge__label">IMSI</span>
        <span class="sim-badge__value sim-badge__value--mono">{{ sim.imsi }}</span>
      </div>
    </template>

    <template v-else>
      <span class="sim-badge__compact-info">
        {{ sim.msisdn ?? sim.iccid.slice(-6) }}
        <span v-if="sim.operator_name" style="margin-left: 4px; opacity: 0.6">{{ sim.operator_name }}</span>
      </span>
    </template>
  </div>
</template>

<style scoped lang="scss">
.sim-badge {
  padding: 8px 12px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  font-size: 13px;

  &__header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
  }

  &__operator {
    font-weight: 500;
    color: var(--el-text-color-primary);
  }

  &__row {
    display: flex;
    gap: 8px;
    margin-top: 4px;
  }

  &__label {
    color: var(--el-text-color-secondary);
    min-width: 44px;
  }

  &__value {
    color: var(--el-text-color-regular);

    &--mono {
      font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
      font-size: 12px;
    }
  }

  &--compact {
    padding: 6px 10px;
  }

  &__compact-info {
    font-size: 12px;
    color: var(--el-text-color-regular);
  }
}
</style>
