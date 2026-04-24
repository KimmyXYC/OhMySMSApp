import client from './client'
import type { Modem, SignalSample } from '@/types/api'

/** 获取所有 modem */
export function listModems() {
  return client.get<Modem[]>('/modems')
}

/** 获取单个 modem 详情 */
export function getModem(id: number) {
  return client.get<Modem>(`/modems/${id}`)
}

/** 获取 modem 最新信号 */
export function getModemSignal(modemId: number) {
  return client.get<SignalSample>(`/modems/${modemId}/signal`)
}

/** 获取 modem 信号历史 */
export function getSignalHistory(modemId: number, limit = 60) {
  return client.get<SignalSample[]>(`/modems/${modemId}/signal/history`, {
    params: { limit },
  })
}
