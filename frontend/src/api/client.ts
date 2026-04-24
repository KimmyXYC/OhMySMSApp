import axios from 'axios'
import type { AxiosInstance } from 'axios'
import router from '@/router'
import { useBackendStore } from '@/stores/backend'

let client: AxiosInstance = createClient()

function createClient(): AxiosInstance {
  // At module load time, Pinia may not be ready yet.
  // We create a default instance; it will be rebuilt after app init.
  const baseURL = localStorage.getItem('ohmysms_backend') || ''
  const resolvedBase = (!baseURL || baseURL === '/')
    ? '/api'
    : baseURL.replace(/\/+$/, '') + '/api'

  const instance = axios.create({
    baseURL: resolvedBase,
    timeout: 15_000,
    headers: {
      'Content-Type': 'application/json',
    },
  })

  // ─── 请求拦截：自动注入 JWT ───
  instance.interceptors.request.use((config) => {
    let token: string | null = null
    try {
      const backendStore = useBackendStore()
      token = backendStore.getToken()
    } catch {
      // Pinia not ready yet, fallback to manual read
      const backend = localStorage.getItem('ohmysms_backend') || ''
      const key = backend || window.location.origin
      token = localStorage.getItem(`ohmysms_token::${key}`)
    }
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  })

  // ─── 响应拦截：401 自动跳登录（保留后端地址）───
  instance.interceptors.response.use(
    (response) => response,
    (error) => {
      if (error.response?.status === 401) {
        try {
          const backendStore = useBackendStore()
          backendStore.clearToken()
        } catch {
          // fallback
          const backend = localStorage.getItem('ohmysms_backend') || ''
          const key = backend || window.location.origin
          localStorage.removeItem(`ohmysms_token::${key}`)
        }
        router.push({ name: 'login' })
      }
      return Promise.reject(error)
    },
  )

  return instance
}

/** Rebuild the axios instance after backend URL changes */
export function rebuildClient() {
  const backendStore = useBackendStore()
  const resolvedBase = backendStore.resolvedBaseURL

  const instance = axios.create({
    baseURL: resolvedBase,
    timeout: 15_000,
    headers: {
      'Content-Type': 'application/json',
    },
  })

  instance.interceptors.request.use((config) => {
    const token = backendStore.getToken()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  })

  instance.interceptors.response.use(
    (response) => response,
    (error) => {
      if (error.response?.status === 401) {
        backendStore.clearToken()
        router.push({ name: 'login' })
      }
      return Promise.reject(error)
    },
  )

  client = instance
}

/** Get the current axios client (always use this, don't cache the import) */
export function getClient(): AxiosInstance {
  return client
}

export default client
