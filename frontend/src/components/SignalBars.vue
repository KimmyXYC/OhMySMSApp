<script setup lang="ts">
defineProps<{
  /** 信号质量百分比 0-100，null 表示未知 */
  quality: number | null
}>()

function getLevel(q: number | null): number {
  if (q === null || q < 0) return 0
  if (q >= 80) return 4
  if (q >= 60) return 3
  if (q >= 40) return 2
  if (q >= 20) return 1
  return 0
}

function getColor(q: number | null): string {
  if (q === null) return 'var(--el-text-color-disabled)'
  if (q >= 60) return 'var(--ohmysms-signal-strong)'
  if (q >= 30) return 'var(--ohmysms-signal-mid)'
  return 'var(--ohmysms-signal-weak)'
}
</script>

<template>
  <div class="signal-bars" :title="quality !== null ? `${quality}%` : '未知'">
    <div
      v-for="i in 4"
      :key="i"
      class="signal-bars__bar"
      :style="{
        height: `${i * 25}%`,
        backgroundColor: i <= getLevel(quality) ? getColor(quality) : 'var(--el-border-color-lighter)',
      }"
    />
    <span v-if="quality !== null" class="signal-bars__text">{{ quality }}%</span>
    <span v-else class="signal-bars__text signal-bars__text--unknown">—</span>
  </div>
</template>

<style scoped lang="scss">
.signal-bars {
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 20px;
  cursor: default;

  &__bar {
    width: 4px;
    border-radius: 1px;
    transition: background-color 0.3s;
  }

  &__text {
    font-size: 11px;
    margin-left: 4px;
    color: var(--el-text-color-regular);
    line-height: 20px;

    &--unknown {
      color: var(--el-text-color-disabled);
    }
  }
}
</style>
