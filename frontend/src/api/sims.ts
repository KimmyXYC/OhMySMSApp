import { getClient } from './client'
import type { SimRow, ListResponse } from '@/types/api'

/** 获取所有 SIM */
export function listSims() {
  return getClient().get<ListResponse<SimRow>>('/sims')
}

/** 获取单个 SIM */
export function getSim(id: number) {
  return getClient().get<SimRow>(`/sims/${id}`)
}

/** 设置 ohmysmsapp 本地显示号码；传空字符串表示清除手动覆盖 */
export function setSimMsisdn(id: number, msisdn: string) {
  return getClient().put<SimRow>(`/sims/${id}/msisdn`, { msisdn })
}

/** 删除未使用 SIM */
export function deleteSim(id: number) {
  return getClient().delete<{ message: string }>(`/sims/${id}`)
}
