<script setup lang="ts">
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useWebSocket } from '@/composables/useWebSocket'

const authStore = useAuthStore()
const { connect, disconnect } = useWebSocket()

onMounted(() => {
  // 若已有 token，尝试连接 WebSocket
  if (authStore.token) {
    connect()
  }
})
</script>

<template>
  <router-view />
</template>

<style>
#app {
  min-height: 100vh;
}
</style>
