<script setup lang="ts">
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useBackendStore } from '@/stores/backend'
import { useWebSocket } from '@/composables/useWebSocket'
import { rebuildClient } from '@/api/client'

const authStore = useAuthStore()
const backendStore = useBackendStore()
const { connect } = useWebSocket()

onMounted(() => {
  // Rebuild client with current backend URL from store
  rebuildClient()
  // Sync token for current backend
  authStore.syncToken()

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
