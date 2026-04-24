import { getClient } from './client'
import type { UssdResponse, USSDRow, ListResponse } from '@/types/api'

/** 发起 USSD */
export function initiateUssd(data: { device_id: string; command: string }) {
  return getClient().post<UssdResponse>('/ussd', data)
}

/** 回复 USSD 会话 */
export function respondUssd(sessionId: string, response: string) {
  return getClient().post<UssdResponse>(`/ussd/${encodeURIComponent(sessionId)}/respond`, { response })
}

/** 取消 USSD 会话 */
export function cancelUssd(sessionId: string) {
  return getClient().delete(`/ussd/${encodeURIComponent(sessionId)}`)
}

/** 获取 USSD 会话历史 */
export function listUssdSessions(limit = 50) {
  return getClient().get<ListResponse<USSDRow>>('/ussd/sessions', { params: { limit } })
}

/** 获取单个 USSD 会话 */
export function getUssdSession(id: number) {
  return getClient().get<USSDRow>(`/ussd/sessions/${id}`)
}
