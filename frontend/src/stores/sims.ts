import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listSims, deleteSim } from '@/api/sims'
import type { SimRow } from '@/types/api'

export const useSimsStore = defineStore('sims', () => {
  const sims = ref<SimRow[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  function $reset() {
    sims.value = []
    loading.value = false
    error.value = null
  }

  async function fetchSims() {
    loading.value = true
    error.value = null
    try {
      const { data } = await listSims()
      sims.value = data.items ?? []
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message || '获取 SIM 列表失败'
    } finally {
      loading.value = false
    }
  }

  function upsertSim(sim: SimRow) {
    const idx = sims.value.findIndex((s) => s.id === sim.id)
    if (idx >= 0) {
      sims.value[idx] = sim
    } else {
      sims.value.push(sim)
    }
  }

  async function doDeleteSim(id: number) {
    await deleteSim(id)
    sims.value = sims.value.filter((s) => s.id !== id)
  }

  return {
    sims,
    loading,
    error,
    $reset,
    fetchSims,
    upsertSim,
    doDeleteSim,
  }
})
