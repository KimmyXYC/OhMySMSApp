import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { listModems, getModem, deleteModem } from '@/api/modems'
import type { ModemRow, ModemState, SignalSample } from '@/types/api'

export const useModemsStore = defineStore('modems', () => {
  const modems = ref<ModemRow[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  const onlineModems = computed(() => modems.value.filter((m) => m.present))
  const offlineModems = computed(() => modems.value.filter((m) => !m.present))
  const modemsWithSim = computed(() => modems.value.filter((m) => m.sim))

  function $reset() {
    modems.value = []
    loading.value = false
    error.value = null
  }

  async function fetchModems() {
    loading.value = true
    error.value = null
    try {
      const { data } = await listModems()
      modems.value = data.items ?? []
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message || '获取模块列表失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchModem(deviceId: string) {
    const { data } = await getModem(deviceId)
    const idx = modems.value.findIndex((m) => m.device_id === deviceId)
    if (idx >= 0) {
      modems.value[idx] = data
    } else {
      modems.value.push(data)
    }
    return data
  }

  async function doDeleteModem(deviceId: string) {
    await deleteModem(deviceId)
    modems.value = modems.value.filter((m) => m.device_id !== deviceId)
  }

  /** WS modem.added / modem.updated → 用 ModemState 快照更新本地列表 */
  function handleModemState(state: ModemState) {
    const idx = modems.value.findIndex((m) => m.device_id === state.device_id)
    if (idx >= 0) {
      // 就地合并核心字段
      const m = modems.value[idx]
      m.manufacturer = state.manufacturer || m.manufacturer
      m.model = state.model || m.model
      m.firmware = state.revision || m.firmware
      m.imei = state.imei || m.imei
      m.primary_port = state.primary_port || m.primary_port
      m.usb_path = state.usb_path || m.usb_path
      m.present = true
      if (!state.has_sim || state.sim === null) {
        m.sim = null
      } else if (state.sim && (!m.sim || m.sim.iccid !== state.sim.iccid)) {
        // WS 的 SimState 不含 DB row id/esim 关联等完整字段；ICCID 变化时拉一次完整行。
        fetchModems()
      }
      // 信号
      if (m.signal) {
        m.signal.quality_pct = state.signal_quality
        m.signal.access_tech = state.access_tech?.[0] ?? m.signal.access_tech
      } else if (state.signal_quality > 0) {
        m.signal = {
          id: 0,
          modem_id: m.id,
          sim_id: null,
          quality_pct: state.signal_quality,
          rssi_dbm: null,
          rsrp_dbm: null,
          rsrq_db: null,
          snr_db: null,
          access_tech: state.access_tech?.[0] ?? null,
          registration: state.registration || null,
          operator_id: state.operator_id || null,
          operator_name: state.operator_name || null,
          sampled_at: new Date().toISOString(),
        }
      }
    } else {
      // 新 modem，fetchModems 获取完整数据
      fetchModems()
    }
  }

  /** WS modem.removed */
  function handleModemRemoved(deviceId: string) {
    const idx = modems.value.findIndex((m) => m.device_id === deviceId)
    if (idx >= 0) {
      modems.value[idx].present = false
      modems.value[idx].sim = null
    }
  }

  /** WS signal.sample 实时更新 */
  function handleSignalSample(sample: SignalSample) {
    const modem = modems.value.find((m) => m.device_id === sample.device_id)
    if (modem) {
      modem.signal = {
        id: 0,
        modem_id: modem.id,
        sim_id: null,
        quality_pct: sample.quality_pct,
        rssi_dbm: sample.rssi_dbm,
        rsrp_dbm: sample.rsrp_dbm,
        rsrq_db: sample.rsrq_db,
        snr_db: sample.snr_db,
        access_tech: sample.access_tech || null,
        registration: sample.registration || null,
        operator_id: sample.operator_id || null,
        operator_name: sample.operator_name || null,
        sampled_at: sample.sampled_at,
      }
    }
  }

  return {
    modems,
    loading,
    error,
    onlineModems,
    offlineModems,
    modemsWithSim,
    $reset,
    fetchModems,
    fetchModem,
    doDeleteModem,
    handleModemState,
    handleModemRemoved,
    handleSignalSample,
  }
})
