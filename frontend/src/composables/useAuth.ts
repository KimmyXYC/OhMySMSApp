import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useWebSocket } from './useWebSocket'
import type { LoginRequest } from '@/types/api'

/**
 * 封装登录/登出流程的 composable
 * 预留 i18n 接口：后续可注入 useI18n() 替换硬编码文案
 */
export function useAuth() {
  const authStore = useAuthStore()
  const router = useRouter()
  const { connect, disconnect } = useWebSocket()

  const isAuthenticated = computed(() => authStore.isAuthenticated)

  async function login(credentials: LoginRequest) {
    await authStore.login(credentials)
    connect()
    const redirect = (router.currentRoute.value.query.redirect as string) || '/'
    await router.push(redirect)
  }

  async function logout() {
    disconnect()
    authStore.logout()
    await router.push({ name: 'login' })
  }

  return {
    isAuthenticated,
    login,
    logout,
  }
}
