import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useBackendStore } from '@/stores/backend'
import { useModemsStore } from '@/stores/modems'
import { useSimsStore } from '@/stores/sims'
import { useSmsStore } from '@/stores/sms'
import { useWebSocket } from './useWebSocket'
import { rebuildClient } from '@/api/client'
import type { LoginRequest } from '@/types/api'

/**
 * 封装登录/登出/切换后端流程的 composable
 */
export function useAuth() {
  const authStore = useAuthStore()
  const backendStore = useBackendStore()
  const router = useRouter()
  const { connect, disconnect } = useWebSocket()

  const isAuthenticated = computed(() => authStore.isAuthenticated)

  async function login(credentials: LoginRequest) {
    await authStore.login(credentials)
    connect()
    const redirect = (router.currentRoute.value.query.redirect as string) || '/dashboard'
    await router.push(redirect)
  }

  async function logout() {
    disconnect()
    authStore.logout()
    await router.push({ name: 'login' })
  }

  /** Switch to a different backend — resets all data stores */
  async function switchBackend(url: string) {
    // 1. Disconnect WS
    disconnect()

    // 2. Switch backend URL
    backendStore.setCurrent(url)

    // 3. Rebuild HTTP client
    rebuildClient()

    // 4. Sync auth token for new backend
    authStore.syncToken()

    // 5. Reset all data stores (avoid stale data from old backend)
    const modemsStore = useModemsStore()
    const simsStore = useSimsStore()
    const smsStore = useSmsStore()

    modemsStore.$reset()
    simsStore.$reset()
    smsStore.$reset()

    // 6. Check if we have a valid token for the new backend
    if (authStore.isAuthenticated) {
      // Reconnect WS
      connect()
      // Redirect to dashboard
      await router.push({ name: 'dashboard' })
    } else {
      // No token for this backend, go to login
      await router.push({ name: 'login' })
    }
  }

  return {
    isAuthenticated,
    login,
    logout,
    switchBackend,
  }
}
