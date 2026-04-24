<script setup lang="ts">
import { ref, nextTick, watch, onMounted } from 'vue'
import type { SMSRow } from '@/types/api'

const props = defineProps<{
  messages: SMSRow[]
  peer: string
}>()

const emit = defineEmits<{
  (e: 'delete', id: number): void
}>()

const scrollContainer = ref<HTMLElement>()

function formatTime(ts: string): string {
  const d = new Date(ts)
  return d.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function scrollToBottom() {
  nextTick(() => {
    if (scrollContainer.value) {
      scrollContainer.value.scrollTop = scrollContainer.value.scrollHeight
    }
  })
}

function handleContextMenu(e: MouseEvent, msg: SMSRow) {
  e.preventDefault()
  if (confirm(`删除这条消息？\n"${msg.body.slice(0, 50)}..."`)) {
    emit('delete', msg.id)
  }
}

watch(
  () => props.messages.length,
  () => scrollToBottom(),
)

onMounted(() => scrollToBottom())
</script>

<template>
  <div class="sms-thread">
    <div class="sms-thread__header">
      <span class="sms-thread__peer">{{ peer }}</span>
    </div>

    <div ref="scrollContainer" class="sms-thread__messages">
      <div
        v-for="msg in messages"
        :key="msg.id"
        class="sms-thread__bubble"
        :class="{
          'sms-thread__bubble--outbound': msg.direction === 'outbound',
          'sms-thread__bubble--inbound': msg.direction === 'inbound',
        }"
        @contextmenu="handleContextMenu($event, msg)"
      >
        <div class="sms-thread__bubble-body">{{ msg.body }}</div>
        <div class="sms-thread__bubble-meta">
          <span>{{ formatTime(msg.ts_created) }}</span>
          <el-tag
            v-if="msg.direction === 'outbound'"
            :type="msg.state === 'sent' ? 'success' : msg.state === 'failed' ? 'danger' : 'info'"
            size="small"
          >
            {{ msg.state === 'sent' ? '已发送' : msg.state === 'failed' ? '失败' : msg.state === 'sending' ? '发送中' : msg.state }}
          </el-tag>
        </div>
      </div>

      <el-empty v-if="messages.length === 0" description="暂无消息" :image-size="60" />
    </div>
  </div>
</template>

<style scoped lang="scss">
.sms-thread {
  display: flex;
  flex-direction: column;
  height: 100%;

  &__header {
    padding: 12px 0;
    border-bottom: 1px solid var(--el-border-color-light);
    margin-bottom: 16px;
    flex-shrink: 0;
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
    overflow-y: auto;
    flex: 1;
    min-height: 0;
  }

  &__bubble {
    max-width: 75%;
    padding: 10px 14px;
    border-radius: 16px;
    font-size: 14px;
    line-height: 1.5;
    cursor: default;
    user-select: text;

    &--inbound {
      align-self: flex-start;
      background: var(--ohmysms-bubble-inbound);
      border-bottom-left-radius: 4px;
    }

    &--outbound {
      align-self: flex-end;
      background: var(--ohmysms-bubble-outbound);
      border-bottom-right-radius: 4px;
    }
  }

  &__bubble-body {
    word-break: break-word;
    white-space: pre-wrap;
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
