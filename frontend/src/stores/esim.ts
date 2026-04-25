import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import {
  listCards,
  getCard,
  listCardProfiles,
  discoverCard,
  addProfile,
  setCardNickname,
  enableProfile,
  disableProfile,
  deleteProfile,
  setProfileNickname,
} from '@/api/esim'
import type { ESimAddProfileRequest, ESimCard, ESimCardDetail, ESimProfile } from '@/types/api'

export const useESimStore = defineStore('esim', () => {
  // ─── State ───
  const cards = ref<ESimCard[]>([])
  const selectedCardId = ref<number | null>(null)
  const selectedCardDetail = ref<ESimCardDetail | null>(null)
  const profiles = ref<ESimProfile[]>([])

  const loading = ref(false)
  const loadingDetail = ref(false)
  const error = ref<string | null>(null)

  /** 正在操作中的 card ID（profile 切换等） */
  const operatingCardId = ref<number | null>(null)
  const operatingText = ref('')

  // ─── Computed ───
  const selectedCard = computed(() =>
    cards.value.find((c) => c.id === selectedCardId.value) ?? null,
  )

  // ─── Actions ───

  function $reset() {
    cards.value = []
    selectedCardId.value = null
    selectedCardDetail.value = null
    profiles.value = []
    loading.value = false
    loadingDetail.value = false
    error.value = null
    operatingCardId.value = null
    operatingText.value = ''
  }

  async function fetchCards() {
    loading.value = true
    error.value = null
    try {
      const { data } = await listCards()
      cards.value = data.items ?? []
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message || '获取 eSIM 列表失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchCardDetail(cardId: number) {
    loadingDetail.value = true
    try {
      const { data } = await getCard(cardId)
      selectedCardDetail.value = data
      profiles.value = data.profiles ?? []
      // 同步更新 cards 列表中对应的条目
      const idx = cards.value.findIndex((c) => c.id === cardId)
      if (idx >= 0) {
        // 保留 profiles 之外的字段
        const { profiles: _p, ...cardFields } = data
        cards.value[idx] = { ...cards.value[idx], ...cardFields }
      }
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message || '获取 eSIM 详情失败'
    } finally {
      loadingDetail.value = false
    }
  }

  async function fetchProfiles(cardId: number) {
    try {
      const { data } = await listCardProfiles(cardId)
      profiles.value = data.items ?? []
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message || '获取 Profile 列表失败'
    }
  }

  function selectCard(cardId: number | null) {
    selectedCardId.value = cardId
    if (cardId !== null) {
      fetchCardDetail(cardId)
    } else {
      selectedCardDetail.value = null
      profiles.value = []
    }
  }

  async function doDiscover(cardId: number) {
    await discoverCard(cardId)
  }

  async function doAddProfile(cardId: number, payload: ESimAddProfileRequest) {
    const { data } = await addProfile(cardId, payload)
    selectedCardDetail.value = data
    profiles.value = data.profiles ?? []
    const idx = cards.value.findIndex((c) => c.id === cardId)
    if (idx >= 0) {
      const { profiles: _p, ...cardFields } = data
      cards.value[idx] = { ...cards.value[idx], ...cardFields }
    }
    return data
  }

  async function doSetCardNickname(cardId: number, nickname: string) {
    const { data } = await setCardNickname(cardId, nickname)
    // 更新本地
    const idx = cards.value.findIndex((c) => c.id === cardId)
    if (idx >= 0) {
      cards.value[idx] = { ...cards.value[idx], ...data }
    }
    if (selectedCardDetail.value?.id === cardId) {
      selectedCardDetail.value = { ...selectedCardDetail.value, ...data }
    }
    return data
  }

  async function doEnableProfile(iccid: string) {
    await enableProfile(iccid)
  }

  async function doDisableProfile(iccid: string) {
    await disableProfile(iccid)
  }

  async function doDeleteProfile(iccid: string, confirmName: string) {
    await deleteProfile(iccid, confirmName)
    profiles.value = profiles.value.filter((p) => p.iccid !== iccid)
    if (selectedCardId.value !== null) {
      await fetchCardDetail(selectedCardId.value)
    }
  }

  async function doSetProfileNickname(iccid: string, nickname: string) {
    const { data } = await setProfileNickname(iccid, nickname)
    // 更新本地
    const idx = profiles.value.findIndex((p) => p.iccid === iccid)
    if (idx >= 0) {
      profiles.value[idx] = { ...profiles.value[idx], ...data }
    }
    return data
  }

  /**
   * Poll card detail 直到 active_iccid 变化。
   * 返回 true 表示成功（已变化），false 表示超时。
   */
  async function pollUntilProfileChange(
    cardId: number,
    targetIccid: string | null,
    timeoutMs = 60_000,
    intervalMs = 5_000,
  ): Promise<boolean> {
    const start = Date.now()
    while (Date.now() - start < timeoutMs) {
      await new Promise((r) => setTimeout(r, intervalMs))
      try {
        const { data } = await getCard(cardId)
        // 更新本地状态
        selectedCardDetail.value = data
        profiles.value = data.profiles ?? []
        const idx = cards.value.findIndex((c) => c.id === cardId)
        if (idx >= 0) {
          const { profiles: _p, ...cardFields } = data
          cards.value[idx] = { ...cards.value[idx], ...cardFields }
        }
        // 判断条件
        if (targetIccid === null) {
          // disable: active_iccid 变为 null
          if (!data.active_iccid) return true
        } else {
          // enable: active_iccid === targetIccid
          if (data.active_iccid === targetIccid) return true
        }
      } catch (e: any) {
        const code = e?.response?.data?.code
        if (code && code !== 'modem_offline') {
          // 非瞬时离线错误也先忽略，让轮询继续等待恢复
        }
      }
    }
    return false
  }

  return {
    // state
    cards,
    selectedCardId,
    selectedCardDetail,
    profiles,
    loading,
    loadingDetail,
    error,
    operatingCardId,
    operatingText,
    // computed
    selectedCard,
    // actions
    $reset,
    fetchCards,
    fetchCardDetail,
    fetchProfiles,
    selectCard,
    doDiscover,
    doAddProfile,
    doSetCardNickname,
    doEnableProfile,
    doDisableProfile,
    doDeleteProfile,
    doSetProfileNickname,
    pollUntilProfileChange,
  }
})
