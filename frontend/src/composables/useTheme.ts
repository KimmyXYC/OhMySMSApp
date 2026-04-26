import { computed, readonly, ref } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'

const THEME_STORAGE_KEY = 'ohmysms_theme_mode'

const themeMode = ref<ThemeMode>('system')
const systemPrefersDark = ref(false)
const initialized = ref(false)

let mediaQuery: MediaQueryList | undefined

function isThemeMode(value: string | null): value is ThemeMode {
  return value === 'light' || value === 'dark' || value === 'system'
}

function readStoredThemeMode(): ThemeMode {
  if (typeof window === 'undefined') return 'system'

  try {
    const value = window.localStorage.getItem(THEME_STORAGE_KEY)
    return isThemeMode(value) ? value : 'system'
  } catch {
    return 'system'
  }
}

function persistThemeMode(mode: ThemeMode) {
  if (typeof window === 'undefined') return

  try {
    if (mode === 'system') {
      window.localStorage.removeItem(THEME_STORAGE_KEY)
    } else {
      window.localStorage.setItem(THEME_STORAGE_KEY, mode)
    }
  } catch {
    // Ignore storage failures, e.g. private mode or disabled storage.
  }
}

function applyTheme() {
  if (typeof document === 'undefined') return

  const shouldUseDark = themeMode.value === 'dark' || (themeMode.value === 'system' && systemPrefersDark.value)

  document.documentElement.classList.toggle('dark', shouldUseDark)
  document.documentElement.style.colorScheme = shouldUseDark ? 'dark' : 'light'
}

function handleSystemThemeChange(event: MediaQueryListEvent) {
  systemPrefersDark.value = event.matches

  if (themeMode.value === 'system') {
    applyTheme()
  }
}

export function initializeTheme() {
  if (initialized.value || typeof window === 'undefined') return

  mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
  systemPrefersDark.value = mediaQuery.matches
  themeMode.value = readStoredThemeMode()
  applyTheme()

  mediaQuery.addEventListener('change', handleSystemThemeChange)
  initialized.value = true
}

const resolvedTheme = computed<'light' | 'dark'>(() => {
  if (themeMode.value === 'system') {
    return systemPrefersDark.value ? 'dark' : 'light'
  }

  return themeMode.value
})

const isDark = computed(() => resolvedTheme.value === 'dark')

function setThemeMode(mode: ThemeMode) {
  initializeTheme()
  themeMode.value = mode
  persistThemeMode(mode)
  applyTheme()
}

function toggleTheme() {
  setThemeMode(isDark.value ? 'light' : 'dark')
}

export function useTheme() {
  initializeTheme()

  return {
    themeMode: readonly(themeMode),
    systemPrefersDark: readonly(systemPrefersDark),
    resolvedTheme,
    isDark,
    setThemeMode,
    toggleTheme,
  }
}
