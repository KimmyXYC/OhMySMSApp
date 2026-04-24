<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useWebSocket } from '@/composables/useWebSocket'
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
} from '@element-plus/icons-vue'

const { logout } = useAuth()
const { connected } = useWebSocket()
const route = useRoute()

const collapsed = ref(false)
const isDark = ref(false)

const menuItems = [
  { index: '/', icon: HomeFilled, label: '控制面板' },
  { index: '/sms', icon: Message, label: '短信' },
  { index: '/ussd', icon: Phone, label: 'USSD' },
  { index: '/esim', icon: CreditCard, label: 'eSIM' },
]

const activeMenu = computed(() => {
  if (route.path.startsWith('/modem/')) return '/'
  return route.path
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
          <!-- WS 状态 -->
          <el-tag :type="connected ? 'success' : 'danger'" size="small" effect="dark" round>
            {{ connected ? '已连接' : '已断开' }}
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
    background-color: var(--el-bg-color);
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
    color: var(--el-text-color-primary);
  }

  &__menu {
    border-right: none;
  }

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 56px;
    border-bottom: 1px solid var(--el-border-color-light);
    padding: 0 16px;
  }

  &__header-left,
  &__header-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  &__main {
    background-color: var(--el-bg-color-page);
    min-height: calc(100vh - 56px);
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
