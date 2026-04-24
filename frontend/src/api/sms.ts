import { getClient } from './client'
import type { SMSRow, ThreadRow, SmsSendRequest, ListResponse } from '@/types/api'

export interface SmsListParams {
  sim_id?: number
  device_id?: string
  peer?: string
  direction?: string
  since?: string
  limit?: number
  offset?: number
}

/** 获取短信列表（支持过滤） */
export function listSms(params: SmsListParams) {
  return getClient().get<ListResponse<SMSRow>>('/sms', { params })
}

/** 获取会话列表 */
export function listThreads(params?: { sim_id?: number; device_id?: string }) {
  return getClient().get<ListResponse<ThreadRow>>('/sms/threads', { params })
}

/** 发送短信 */
export function sendSms(data: SmsSendRequest) {
  return getClient().post<SMSRow>('/sms/send', data)
}

/** 删除短信 */
export function deleteSms(id: number) {
  return getClient().delete(`/sms/${id}`)
}
