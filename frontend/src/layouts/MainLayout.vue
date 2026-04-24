<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useWebSocket } from '@/composables/useWebSocket'
import type { WsStatus } from '@/composables/useWebSocket'
import BackendSwitcher from '@/components/BackendSwitcher.vue'
import {
  HomeFilled,
  Message,
  Phone,
  CreditCard,
  Expand,
  Fold,
  SwitchButton,
  Moon,
  Sunny,
  Setting,
  Iphone,
} from '@element-plus/icons-vue'

const { logout } = useAuth()
const { status } = useWebSocket()
const route = useRoute()

const collapsed = ref(false)
const isDark = ref(document.documentElement.classList.contains('dark'))

const menuItems = [
  { index: '/dashboard', icon: HomeFilled, label: '控制面板' },
  { index: '/sms', icon: Message, label: '短信' },
  { index: '/ussd', icon: Phone, label: 'USSD' },
  { index: '/sims', icon: Iphone, label: 'SIM 卡' },
  { index: '/esim', icon: CreditCard, label: 'eSIM' },
  { index: '/settings', icon: Setting, label: '设置' },
]

const activeMenu = computed(() => {
  if (route.path.startsWith('/modems/')) return '/dashboard'
  return route.path
})

const wsTagType = computed(() => {
  const map: Record<WsStatus, '' | 'warning' | 'danger'> = {
    connected: '',
    reconnecting: 'warning',
    disconnected: 'danger',
  }
  return map[status.value] || 'danger'
})

const wsLabel = computed(() => {
  const map: Record<WsStatus, string> = {
    connected: '已连接',
    reconnecting: '重连中',
    disconnected: '已断开',
  }
  return map[status.value] || '已断开'
})

function toggleDark() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
}
</script>

<template>
  <el-container class="main-layout">
    <!-- 侧栏 -->
    <el-aside :width="collapsed ? '64px' : '200px'" class="main-layout__aside">
      <div class="main-layout__logo">
        <img src="/favicon.svg" alt="OhMySMS" width="32" height="32" />
        <span v-show="!collapsed" class="main-layout__logo-text">OhMySMS</span>
      </div>

      <el-menu
        :default-active="activeMenu"
        :collapse="collapsed"
        router
        class="main-layout__menu"
      >
        <el-menu-item v-for="item in menuItems" :key="item.index" :index="item.index">
          <el-icon><component :is="item.icon" /></el-icon>
          <template #title>{{ item.label }}</template>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <!-- 顶栏 -->
      <el-header class="main-layout__header">
        <div class="main-layout__header-left">
          <el-button text @click="collapsed = !collapsed">
            <el-icon :size="18">
              <Fold v-if="!collapsed" />
              <Expand v-else />
            </el-icon>
          </el-button>
        </div>

        <div class="main-layout__header-right">
          <!-- 后端切换 -->
          <BackendSwitcher />

          <!-- WS 状态 -->
          <el-tag :type="wsTagType" size="small" effect="dark" round class="ws-tag">
            <span class="ws-dot" :class="'ws-dot--' + status" />
            {{ wsLabel }}
          </el-tag>

          <!-- 深浅色切换 -->
          <el-button text @click="toggleDark">
            <el-icon :size="18">
              <Moon v-if="!isDark" />
              <Sunny v-else />
            </el-icon>
          </el-button>

          <!-- 登出 -->
          <el-button text @click="logout">
            <el-icon :size="18"><SwitchButton /></el-icon>
          </el-button>
        </div>
      </el-header>

      <!-- 主内容 -->
      <el-main class="main-layout__main">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped lang="scss">
.main-layout {
  min-height: 100vh;

  &__aside {
    background-color: var(--ohmysms-bg-aside);
    border-right: 1px solid var(--el-border-color-light);
    transition: width 0.3s;
    overflow: hidden;
  }

  &__logo {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 16px;
    height: 56px;
    overflow: hidden;
  }

  &__logo-text {
    font-size: 18px;
    font-weight: 700;
    letter-spacing: -0.5px;
    white-space: nowrap;
    color: var(--ohmysms-primary);
  }

  &__menu {
    border-right: none;

    // Override Element Plus menu active/hover states
    :deep(.el-menu-item.is-active) {
      color: var(--ohmysms-primary);
      background-color: var(--ohmysms-primary-light);
    }

    :deep(.el-menu-item:hover) {
      background-color: var(--ohmysms-primary-light-2);
    }
  }

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 56px;
    border-bottom: 1px solid var(--el-border-color-light);
    padding: 0 16px;
    background-color: var(--ohmysms-bg-header);
  }

  &__header-left,
  &__header-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  &__main {
    background-color: var(--ohmysms-bg-page);
    min-height: calc(100vh - 56px);
  }
}

.ws-tag {
  // connected → primary style (light blue)
  &:not(.el-tag--warning):not(.el-tag--danger) {
    --el-tag-bg-color: var(--ohmysms-primary-light);
    --el-tag-border-color: var(--ohmysms-primary);
    --el-tag-text-color: var(--ohmysms-primary);
  }
}

.ws-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  margin-right: 4px;
  vertical-align: middle;

  &--connected {
    background-color: var(--ohmysms-ws-connected);
    box-shadow: 0 0 4px var(--ohmysms-online-glow);
  }

  &--reconnecting {
    background-color: var(--ohmysms-ws-reconnecting);
    animation: pulse 1.5s infinite;
  }

  &--disconnected {
    background-color: var(--ohmysms-ws-disconnected);
  }
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.3;
  }
}

/* ─── 响应式 ─── */
@media (max-width: 768px) {
  .main-layout__aside {
    position: fixed;
    z-index: 1000;
    height: 100vh;
    transform: translateX(0);
    transition: transform 0.3s;
  }

  .main-layout__aside[style*='64px'] {
    transform: translateX(-100%);
  }
}
</style>
