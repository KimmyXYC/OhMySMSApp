import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listSims } from '@/api/sims'
import type { Sim } from '@/types/api'

export const useSimsStore = defineStore('sims', () => {
  const sims = ref<Sim[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchSims() {
    loading.value = true
    error.value = null
    try {
      const { data } = await listSims()
      sims.value = data
    } catch (e: any) {
      error.value = e.message || '获取 SIM 列表失败'
    } finally {
      loading.value = false
    }
  }

  function upsertSim(sim: Sim) {
    const idx = sims.value.findIndex((s) => s.id === sim.id)
    if (idx >= 0) {
      sims.value[idx] = sim
    } else {
      sims.value.push(sim)
    }
  }

  return {
    sims,
    loading,
    error,
    fetchSims,
    upsertSim,
  }
})
