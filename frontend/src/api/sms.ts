import client from './client'
import type { Sms, SmsThread, SmsSendRequest, Paginated } from '@/types/api'

/** 获取短信列表（分页） */
export function listSms(params: { sim_id?: number; page?: number; per_page?: number }) {
  return client.get<Paginated<Sms>>('/sms', { params })
}

/** 获取会话列表 */
export function listThreads(simId?: number) {
  return client.get<SmsThread[]>('/sms/threads', {
    params: simId ? { sim_id: simId } : undefined,
  })
}

/** 获取某会话的完整消息 */
export function getThread(simId: number, peer: string) {
  return client.get<Sms[]>('/sms/thread', {
    params: { sim_id: simId, peer },
  })
}

/** 发送短信 */
export function sendSms(data: SmsSendRequest) {
  return client.post<Sms>('/sms/send', data)
}
