<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useWebSocket } from '@/composables/useWebSocket'
import { useTheme, type ThemeMode } from '@/composables/useTheme'
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
const { themeMode, isDark, setThemeMode, toggleTheme } = useTheme()
const route = useRoute()

const collapsed = ref(false)
const isMobile = ref(false)
const mobileMenuOpen = ref(false)

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

const currentTitle = computed(() => {
  return menuItems.find((item) => item.index === activeMenu.value)?.label || 'OhMySMS'
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

const themeButtonTitle = computed(() => {
  const modeLabel: Record<ThemeMode, string> = {
    system: '跟随系统',
    light: '浅色模式',
    dark: '深色模式',
  }
  const resolvedLabel = isDark.value ? '深色' : '浅色'

  return themeMode.value === 'system'
    ? `当前主题：${modeLabel.system}（${resolvedLabel}）`
    : `当前主题：${modeLabel[themeMode.value]}`
})

function handleThemeCommand(command: string | number | object) {
  if (command === 'system' || command === 'light' || command === 'dark') {
    setThemeMode(command)
  }
}

function updateMobile() {
  isMobile.value = window.innerWidth <= 767
  if (!isMobile.value) mobileMenuOpen.value = false
}

function toggleSidebar() {
  if (isMobile.value) {
    mobileMenuOpen.value = !mobileMenuOpen.value
  } else {
    collapsed.value = !collapsed.value
  }
}

function handleMenuSelect() {
  if (isMobile.value) mobileMenuOpen.value = false
}

onMounted(() => {
  updateMobile()
  window.addEventListener('resize', updateMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', updateMobile)
})
</script>

<template>
  <el-container class="main-layout">
    <div v-if="isMobile && mobileMenuOpen" class="main-layout__mobile-mask" @click="mobileMenuOpen = false" />
    <!-- 侧栏 -->
    <el-aside
      :width="collapsed ? '64px' : '200px'"
      class="main-layout__aside"
      :class="{ 'main-layout__aside--mobile-open': isMobile && mobileMenuOpen }"
    >
      <div class="main-layout__logo">
        <img src="/favicon.svg" alt="OhMySMS" width="32" height="32" />
        <span v-show="!collapsed || isMobile" class="main-layout__logo-text">OhMySMS</span>
      </div>

      <el-menu
        :default-active="activeMenu"
        :collapse="!isMobile && collapsed"
        router
        class="main-layout__menu"
        @select="handleMenuSelect"
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
          <el-button text @click="toggleSidebar">
            <el-icon :size="18">
              <Fold v-if="!isMobile && !collapsed" />
              <Expand v-else />
            </el-icon>
          </el-button>
          <span v-if="isMobile" class="main-layout__header-title text-ellipsis">{{ currentTitle }}</span>
        </div>

        <div class="main-layout__header-right">
          <!-- 后端切换 -->
          <BackendSwitcher />

          <!-- WS 状态 -->
          <el-tag v-if="!isMobile" :type="wsTagType" size="small" effect="dark" round class="ws-tag">
            <span class="ws-dot" :class="'ws-dot--' + status" />
            {{ wsLabel }}
          </el-tag>
          <span v-else class="ws-dot" :class="'ws-dot--' + status" />

          <!-- 主题切换 -->
          <el-dropdown trigger="click" @command="handleThemeCommand">
            <el-button text :title="themeButtonTitle" :aria-label="themeButtonTitle" @dblclick="toggleTheme">
              <el-icon :size="18">
                <Moon v-if="isDark" />
                <Sunny v-else />
              </el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="system">
                  <span class="theme-menu-item">
                    <span>跟随系统</span>
                    <span v-if="themeMode === 'system'" class="theme-menu-item__check">✓</span>
                  </span>
                </el-dropdown-item>
                <el-dropdown-item command="light">
                  <span class="theme-menu-item">
                    <span>浅色模式</span>
                    <span v-if="themeMode === 'light'" class="theme-menu-item__check">✓</span>
                  </span>
                </el-dropdown-item>
                <el-dropdown-item command="dark">
                  <span class="theme-menu-item">
                    <span>深色模式</span>
                    <span v-if="themeMode === 'dark'" class="theme-menu-item__check">✓</span>
                  </span>
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>

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
    background:
      linear-gradient(180deg, var(--ohmysms-bg-aside) 0%, var(--ohmysms-bg-aside-soft) 100%);
    border-right: 1px solid var(--ohmysms-sidebar-border);
    transition:
      width 0.3s,
      transform 0.3s,
      background-color 0.3s;
    overflow: hidden;
  }

  &__logo {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 16px;
    height: 56px;
    overflow: hidden;
    border-bottom: 1px solid var(--ohmysms-sidebar-divider);
  }

  &__logo-text {
    font-size: 18px;
    font-weight: 700;
    letter-spacing: -0.5px;
    white-space: nowrap;
    color: var(--ohmysms-primary);
  }

  &__menu {
    --el-menu-bg-color: transparent;
    --el-menu-hover-bg-color: var(--ohmysms-sidebar-hover-bg);
    --el-menu-active-color: var(--ohmysms-sidebar-text-active);
    --el-menu-text-color: var(--ohmysms-sidebar-text);

    padding: 10px 8px;
    border-right: none;
    background: transparent;
    box-sizing: border-box;
    width: 100%;

    :deep(.el-menu-item) {
      height: 42px;
      margin: 3px 0;
      border-radius: 9px;
      color: var(--ohmysms-sidebar-text);
      background-color: transparent;
      line-height: 42px;
      transition:
        color 0.2s ease,
        background-color 0.2s ease;
    }

    // Override Element Plus menu active/hover states
    :deep(.el-menu-item.is-active) {
      color: var(--ohmysms-sidebar-text-active);
      background-color: var(--ohmysms-sidebar-active-bg);
    }

    :deep(.el-menu-item:hover) {
      color: var(--ohmysms-sidebar-text-active);
      background-color: var(--ohmysms-sidebar-hover-bg);
    }

    :deep(.el-menu-item.is-active:hover) {
      background-color: var(--ohmysms-sidebar-active-bg);
    }

    // el-menu--collapse is added to the el-menu root itself. In scoped CSS,
    // keep this as a same-node selector so it compiles to
    // .main-layout__menu.el-menu--collapse[data-v-*] instead of a descendant
    // selector that never matches the root.
    &.el-menu--collapse {
      --el-menu-base-level-padding: 0px;

      width: 100%;
      padding-inline: 0;

      :deep(.el-menu-item) {
        justify-content: center;
        width: 48px;
        min-width: 0;
        margin-inline: auto;
        padding: 0 !important;
      }

      :deep(.el-menu-item .el-icon) {
        display: inline-flex;
        justify-content: center;
        width: 100%;
        margin-inline: 0 !important;
      }
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

.theme-menu-item {
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  min-width: 92px;
  gap: 18px;

  &__check {
    color: var(--ohmysms-primary);
    font-weight: 700;
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
    width: 220px !important;
    transform: translateX(-100%);
  }

  .main-layout__aside--mobile-open {
    transform: translateX(0);
  }

  .main-layout__mobile-mask {
    position: fixed;
    inset: 0;
    background: var(--ohmysms-overlay-mask);
    z-index: 999;
  }

  .main-layout__header {
    padding: 0 12px;
  }

  .main-layout__header-title {
    font-size: 16px;
    font-weight: 600;
    max-width: 120px;
  }

  .main-layout__header-right {
    gap: 4px;
  }

  .main-layout__main {
    padding: 0;
  }
}
</style>
