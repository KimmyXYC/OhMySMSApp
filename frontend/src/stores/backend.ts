import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export interface KnownBackend {
  url: string
  username: string
  addedAt: string
}

const LS_BACKEND_KEY = 'ohmysms_backend'
const LS_KNOWN_KEY = 'ohmysms_known_backends'

function tokenKey(backend: string): string {
  return `ohmysms_token::${backend}`
}

function loadKnown(): KnownBackend[] {
  try {
    const raw = localStorage.getItem(LS_KNOWN_KEY)
    return raw ? JSON.parse(raw) : []
  } catch {
    return []
  }
}

function saveKnown(list: KnownBackend[]) {
  localStorage.setItem(LS_KNOWN_KEY, JSON.stringify(list))
}

export const useBackendStore = defineStore('backend', () => {
  // ─── state ───
  const current = ref<string>(localStorage.getItem(LS_BACKEND_KEY) || '')
  const known = ref<KnownBackend[]>(loadKnown())

  // ─── getters ───
  /** Resolved base URL. Empty string means same-origin (use '/api' relative path). */
  const resolvedBaseURL = computed(() => {
    if (!current.value || current.value === '/') return '/api'
    // Strip trailing slash then append /api
    return current.value.replace(/\/+$/, '') + '/api'
  })

  /** Resolved WS URL prefix (no path). Empty means same-origin. */
  const resolvedWsBase = computed(() => {
    if (!current.value || current.value === '/') {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
      return `${proto}//${location.host}`
    }
    return current.value
      .replace(/\/+$/, '')
      .replace(/^http/, 'ws')
  })

  /** Get token for current backend */
  const currentToken = computed(() => {
    const key = current.value || window.location.origin
    return localStorage.getItem(tokenKey(key))
  })

  /** Whether we're using same-origin mode (Vite proxy in dev) */
  const isSameOrigin = computed(() => !current.value || current.value === '/')

  // ─── actions ───
  function setCurrent(url: string) {
    current.value = url
    localStorage.setItem(LS_BACKEND_KEY, url)
  }

  function setToken(token: string) {
    const key = current.value || window.location.origin
    localStorage.setItem(tokenKey(key), token)
  }

  function getToken(): string | null {
    const key = current.value || window.location.origin
    return localStorage.getItem(tokenKey(key))
  }

  function clearToken() {
    const key = current.value || window.location.origin
    localStorage.removeItem(tokenKey(key))
  }

  function addKnown(url: string, username: string) {
    const idx = known.value.findIndex((b) => b.url === url)
    if (idx >= 0) {
      known.value[idx].username = username
      known.value[idx].addedAt = new Date().toISOString()
    } else {
      known.value.unshift({ url, username, addedAt: new Date().toISOString() })
    }
    // Keep last 10
    if (known.value.length > 10) {
      known.value = known.value.slice(0, 10)
    }
    saveKnown(known.value)
  }

  function forget(url: string) {
    known.value = known.value.filter((b) => b.url !== url)
    saveKnown(known.value)
    localStorage.removeItem(tokenKey(url))
  }

  function switchTo(url: string) {
    setCurrent(url)
  }

  /** Recent 5 known backends for display */
  const recentBackends = computed(() => known.value.slice(0, 5))

  return {
    current,
    known,
    resolvedBaseURL,
    resolvedWsBase,
    currentToken,
    isSameOrigin,
    recentBackends,
    setCurrent,
    setToken,
    getToken,
    clearToken,
    addKnown,
    forget,
    switchTo,
  }
})
