import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { listModems, getModem } from '@/api/modems'
import type { Modem } from '@/types/api'

export const useModemsStore = defineStore('modems', () => {
  const modems = ref<Modem[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  const onlineModems = computed(() => modems.value.filter((m) => m.present))
  const offlineModems = computed(() => modems.value.filter((m) => !m.present))

  async function fetchModems() {
    loading.value = true
    error.value = null
    try {
      const { data } = await listModems()
      modems.value = data
    } catch (e: any) {
      error.value = e.message || '获取模块列表失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchModem(id: number) {
    const { data } = await getModem(id)
    const idx = modems.value.findIndex((m) => m.id === id)
    if (idx >= 0) {
      modems.value[idx] = data
    } else {
      modems.value.push(data)
    }
    return data
  }

  /** WebSocket 推送时更新单个 modem */
  function upsertModem(modem: Modem) {
    const idx = modems.value.findIndex((m) => m.id === modem.id)
    if (idx >= 0) {
      modems.value[idx] = modem
    } else {
      modems.value.push(modem)
    }
  }

  /** modem 被移除 */
  function removeModem(modemId: number) {
    modems.value = modems.value.filter((m) => m.id !== modemId)
  }

  return {
    modems,
    loading,
    error,
    onlineModems,
    offlineModems,
    fetchModems,
    fetchModem,
    upsertModem,
    removeModem,
  }
})
