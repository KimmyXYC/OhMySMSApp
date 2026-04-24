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
