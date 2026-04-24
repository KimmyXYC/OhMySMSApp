import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listThreads, listSms, sendSms as sendSmsApi, deleteSms as deleteSmsApi } from '@/api/sms'
import type { ThreadRow, SMSRow, SmsSendRequest } from '@/types/api'

export const useSmsStore = defineStore('sms', () => {
  const threads = ref<ThreadRow[]>([])
  const currentMessages = ref<SMSRow[]>([])
  const loading = ref(false)
  const sending = ref(false)
  const error = ref<string | null>(null)

  // 前端未读状态 (peer -> unread count)
  const unreadMap = ref<Record<string, number>>({})

  function $reset() {
    threads.value = []
    currentMessages.value = []
    loading.value = false
    sending.value = false
    error.value = null
    unreadMap.value = {}
  }

  async function fetchThreads(params?: { sim_id?: number; device_id?: string }) {
    loading.value = true
    error.value = null
    try {
      const { data } = await listThreads(params)
      threads.value = data.items ?? []
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message || '获取会话列表失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchMessages(params: {
    peer: string
    device_id?: string
    sim_id?: number
    limit?: number
    offset?: number
  }) {
    loading.value = true
    error.value = null
    try {
      const { data } = await listSms({
        peer: params.peer,
        device_id: params.device_id,
        sim_id: params.sim_id,
        limit: params.limit ?? 200,
        offset: params.offset ?? 0,
      })
      // API 返回 DESC，前端需要 ASC 显示
      currentMessages.value = (data.items ?? []).reverse()
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message || '获取消息失败'
    } finally {
      loading.value = false
    }
  }

  async function sendSms(req: SmsSendRequest) {
    sending.value = true
    try {
      const { data } = await sendSmsApi(req)
      // 将返回的消息追加到当前流
      if (data && data.id) {
        currentMessages.value.push(data)
      }
      return data
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message || '发送失败'
      throw e
    } finally {
      sending.value = false
    }
  }

  async function deleteMessage(id: number) {
    await deleteSmsApi(id)
    currentMessages.value = currentMessages.value.filter((m) => m.id !== id)
  }

  /** 标记 peer 未读 +1 */
  function markUnread(peer: string) {
    unreadMap.value[peer] = (unreadMap.value[peer] || 0) + 1
  }

  /** 清除 peer 未读 */
  function clearUnread(peer: string) {
    unreadMap.value[peer] = 0
  }

  /** 刷新当前会话（WS 推送后调用） */
  async function refreshCurrentThread(peer: string, deviceId?: string) {
    const { data } = await listSms({ peer, device_id: deviceId, limit: 200 })
    currentMessages.value = (data.items ?? []).reverse()
  }

  return {
    threads,
    currentMessages,
    loading,
    sending,
    error,
    unreadMap,
    $reset,
    fetchThreads,
    fetchMessages,
    sendSms,
    deleteMessage,
    markUnread,
    clearUnread,
    refreshCurrentThread,
  }
})
