<script setup lang="ts">
import { ref, computed } from 'vue'
import { useBackendStore, type KnownBackend } from '@/stores/backend'
import { useAuth } from '@/composables/useAuth'
import { Connection, Delete, Check } from '@element-plus/icons-vue'

const backendStore = useBackendStore()
const { switchBackend, logout } = useAuth()

const showPopover = ref(false)

const displayCurrent = computed(() => {
  return backendStore.current || window.location.origin + ' (同源)'
})

const knownList = computed(() => backendStore.recentBackends)

async function handleSwitch(backend: KnownBackend) {
  showPopover.value = false
  await switchBackend(backend.url)
}

function handleForget(url: string) {
  backendStore.forget(url)
}

async function handleLogoutCurrent() {
  showPopover.value = false
  await logout()
}

function isCurrent(url: string): boolean {
  return url === backendStore.current
}
</script>

<template>
  <el-popover
    v-model:visible="showPopover"
    placement="bottom-end"
    :width="320"
    trigger="click"
  >
    <template #reference>
      <el-button text class="backend-trigger">
        <el-icon :size="16"><Connection /></el-icon>
        <span class="backend-trigger__text">{{ displayCurrent }}</span>
      </el-button>
    </template>

    <div class="backend-popover">
      <div class="backend-popover__header">
        <span class="backend-popover__title">后端连接</span>
      </div>

      <!-- 当前后端 -->
      <div class="backend-popover__current">
        <div class="backend-popover__current-label">当前后端</div>
        <div class="backend-popover__current-url">{{ displayCurrent }}</div>
        <el-button size="small" type="danger" text @click="handleLogoutCurrent">
          登出当前后端
        </el-button>
      </div>

      <!-- 已保存的后端 -->
      <template v-if="knownList.length > 0">
        <div class="backend-popover__section-title">已保存的后端</div>
        <div
          v-for="item in knownList"
          :key="item.url"
          class="backend-popover__item"
          :class="{ 'backend-popover__item--active': isCurrent(item.url) }"
        >
          <div class="backend-popover__item-info" @click="handleSwitch(item)">
            <el-icon v-if="isCurrent(item.url)" class="backend-popover__item-check">
              <Check />
            </el-icon>
            <div>
              <div class="backend-popover__item-url">{{ item.url || '同源模式' }}</div>
              <div class="backend-popover__item-user">{{ item.username }}</div>
            </div>
          </div>
          <el-button
            v-if="!isCurrent(item.url)"
            text
            size="small"
            class="backend-popover__item-delete"
            @click.stop="handleForget(item.url)"
          >
            <el-icon :size="14"><Delete /></el-icon>
          </el-button>
        </div>
      </template>
    </div>
  </el-popover>
</template>

<style scoped lang="scss">
.backend-trigger {
  max-width: 200px;
  overflow: hidden;

  &__text {
    margin-left: 4px;
    font-size: 12px;
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--el-text-color-secondary);
  }
}

.backend-popover {
  &__header {
    padding-bottom: 8px;
    border-bottom: 1px solid var(--el-border-color-lighter);
    margin-bottom: 12px;
  }

  &__title {
    font-weight: 600;
    font-size: 14px;
  }

  &__current {
    padding: 8px 0;
    margin-bottom: 8px;
  }

  &__current-label {
    font-size: 11px;
    color: var(--el-text-color-placeholder);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 4px;
  }

  &__current-url {
    font-size: 13px;
    font-weight: 500;
    word-break: break-all;
    margin-bottom: 6px;
    color: var(--el-text-color-primary);
  }

  &__section-title {
    font-size: 11px;
    color: var(--el-text-color-placeholder);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin: 12px 0 6px;
    padding-top: 8px;
    border-top: 1px solid var(--el-border-color-lighter);
  }

  &__item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 8px;
    border-radius: 6px;
    cursor: pointer;
    transition: background 0.15s;

    &:hover {
      background: var(--el-fill-color-light);
    }

    &--active {
      background: var(--ohmysms-primary-light, rgba(96, 165, 250, 0.08));
    }
  }

  &__item-info {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
  }

  &__item-check {
    color: var(--el-color-primary);
    flex-shrink: 0;
  }

  &__item-url {
    font-size: 13px;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 200px;
  }

  &__item-user {
    font-size: 11px;
    color: var(--el-text-color-secondary);
  }

  &__item-delete {
    flex-shrink: 0;
    opacity: 0.5;

    &:hover {
      opacity: 1;
    }
  }
}
</style>
