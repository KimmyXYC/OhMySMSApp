import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { TOKEN_KEY } from '@/api/client'
import { login as loginApi } from '@/api/auth'
import type { LoginRequest } from '@/types/api'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem(TOKEN_KEY))
  const username = ref<string | null>(null)

  const isAuthenticated = computed(() => !!token.value)

  async function login(credentials: LoginRequest) {
    const { data } = await loginApi(credentials)
    token.value = data.token
    username.value = credentials.username
    localStorage.setItem(TOKEN_KEY, data.token)
  }

  function logout() {
    token.value = null
    username.value = null
    localStorage.removeItem(TOKEN_KEY)
  }

  return {
    token,
    username,
    isAuthenticated,
    login,
    logout,
  }
})
