import client from './client'
import type { Sim } from '@/types/api'

/** 获取所有 SIM */
export function listSims() {
  return client.get<Sim[]>('/sims')
}

/** 获取单个 SIM 详情 */
export function getSim(id: number) {
  return client.get<Sim>(`/sims/${id}`)
}

/** 获取某 modem 当前绑定的 SIM */
export function getSimByModem(modemId: number) {
  return client.get<Sim>(`/modems/${modemId}/sim`)
}
