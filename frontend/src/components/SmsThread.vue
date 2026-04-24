<script setup lang="ts">
import type { Sms } from '@/types/api'

defineProps<{
  messages: Sms[]
  peer: string
}>()

function formatTime(ts: string): string {
  const d = new Date(ts)
  return d.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<template>
  <div class="sms-thread">
    <div class="sms-thread__header">
      <span class="sms-thread__peer">{{ peer }}</span>
    </div>

    <div class="sms-thread__messages">
      <div
        v-for="msg in messages"
        :key="msg.id"
        class="sms-thread__bubble"
        :class="{
          'sms-thread__bubble--outbound': msg.direction === 'outbound',
          'sms-thread__bubble--inbound': msg.direction === 'inbound',
        }"
      >
        <div class="sms-thread__bubble-body">{{ msg.body }}</div>
        <div class="sms-thread__bubble-meta">
          <span>{{ formatTime(msg.ts_created) }}</span>
          <el-tag
            v-if="msg.direction === 'outbound'"
            :type="msg.state === 'sent' ? 'success' : msg.state === 'failed' ? 'danger' : 'info'"
            size="small"
          >
            {{ msg.state === 'sent' ? '已发送' : msg.state === 'failed' ? '失败' : '发送中' }}
          </el-tag>
        </div>
      </div>

      <el-empty v-if="messages.length === 0" description="暂无消息" :image-size="60" />
    </div>
  </div>
</template>

<style scoped lang="scss">
.sms-thread {
  &__header {
    padding: 12px 0;
    border-bottom: 1px solid var(--el-border-color-light);
    margin-bottom: 16px;
  }

  &__peer {
    font-size: 16px;
    font-weight: 600;
  }

  &__messages {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 0 4px;
  }

  &__bubble {
    max-width: 75%;
    padding: 10px 14px;
    border-radius: 16px;
    font-size: 14px;
    line-height: 1.5;

    &--inbound {
      align-self: flex-start;
      background: var(--el-fill-color-light);
      border-bottom-left-radius: 4px;
    }

    &--outbound {
      align-self: flex-end;
      background: var(--el-color-primary-light-7);
      border-bottom-right-radius: 4px;
    }
  }

  &__bubble-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 4px;
    font-size: 11px;
    color: var(--el-text-color-placeholder);
  }
}
</style>
