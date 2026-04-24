import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listThreads, getThread, sendSms as sendSmsApi } from '@/api/sms'
import type { SmsThread, Sms, SmsSendRequest } from '@/types/api'

export const useSmsStore = defineStore('sms', () => {
  const threads = ref<SmsThread[]>([])
  const currentMessages = ref<Sms[]>([])
  const loading = ref(false)
  const sending = ref(false)
  const error = ref<string | null>(null)

  async function fetchThreads(simId?: number) {
    loading.value = true
    error.value = null
    try {
      const { data } = await listThreads(simId)
      threads.value = data
    } catch (e: any) {
      error.value = e.message || '获取会话列表失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchThread(simId: number, peer: string) {
    loading.value = true
    try {
      const { data } = await getThread(simId, peer)
      currentMessages.value = data
    } catch (e: any) {
      error.value = e.message || '获取消息失败'
    } finally {
      loading.value = false
    }
  }

  async function sendSms(req: SmsSendRequest) {
    sending.value = true
    try {
      const { data } = await sendSmsApi(req)
      currentMessages.value.push(data)
      return data
    } catch (e: any) {
      error.value = e.message || '发送失败'
      throw e
    } finally {
      sending.value = false
    }
  }

  /** WebSocket 推送新短信 */
  function addIncomingSms(sms: Sms) {
    // 更新当前会话
    if (currentMessages.value.length > 0) {
      const first = currentMessages.value[0]
      if (first.sim_id === sms.sim_id && first.peer === sms.peer) {
        currentMessages.value.push(sms)
      }
    }
    // 更新会话列表
    const thread = threads.value.find(
      (t) => t.sim_id === sms.sim_id && t.peer === sms.peer,
    )
    if (thread) {
      thread.last_message = sms
      thread.unread_count++
    }
  }

  return {
    threads,
    currentMessages,
    loading,
    sending,
    error,
    fetchThreads,
    fetchThread,
    sendSms,
    addIncomingSms,
  }
})
