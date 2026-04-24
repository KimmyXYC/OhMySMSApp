import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { useBackendStore } from './backend'
import { login as loginApi, checkAuth } from '@/api/auth'
import type { LoginRequest } from '@/types/api'

export const useAuthStore = defineStore('auth', () => {
  const backendStore = useBackendStore()

  const token = ref<string | null>(backendStore.getToken())
  const username = ref<string | null>(null)

  const isAuthenticated = computed(() => !!token.value)

  /** Refresh token from localStorage (after backend switch) */
  function syncToken() {
    token.value = backendStore.getToken()
  }

  async function login(credentials: LoginRequest) {
    const { data } = await loginApi(credentials)
    token.value = data.token
    username.value = data.user.username
    backendStore.setToken(data.token)
    // Record to known list
    backendStore.addKnown(
      backendStore.current || window.location.origin,
      data.user.username,
    )
  }

  async function fetchMe() {
    try {
      const { data } = await checkAuth()
      username.value = data.username
    } catch {
      // token invalid
      logout()
    }
  }

  function logout() {
    token.value = null
    username.value = null
    backendStore.clearToken()
  }

  return {
    token,
    username,
    isAuthenticated,
    syncToken,
    login,
    fetchMe,
    logout,
  }
})
