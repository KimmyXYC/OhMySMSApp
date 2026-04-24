import client from './client'
import type { UssdSession, UssdInitRequest, UssdReplyRequest } from '@/types/api'

/** 发起 USSD 请求 */
export function startUssd(data: UssdInitRequest) {
  return client.post<UssdSession>('/ussd/start', data)
}

/** 回复 USSD 会话 */
export function replyUssd(data: UssdReplyRequest) {
  return client.post<UssdSession>('/ussd/reply', data)
}

/** 取消 USSD 会话 */
export function cancelUssd(sessionId: number) {
  return client.post<UssdSession>(`/ussd/${sessionId}/cancel`)
}

/** 获取 USSD 会话历史 */
export function listUssdSessions(modemId?: number) {
  return client.get<UssdSession[]>('/ussd/sessions', {
    params: modemId ? { modem_id: modemId } : undefined,
  })
}
