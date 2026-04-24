import { getClient } from './client'
import type { ModemRow, ListResponse } from '@/types/api'

/** 获取所有 modem（含 sim & signal） */
export function listModems() {
  return getClient().get<ListResponse<ModemRow>>('/modems')
}

/** 获取单个 modem */
export function getModem(deviceId: string) {
  return getClient().get<ModemRow>(`/modems/${encodeURIComponent(deviceId)}`)
}

/** 重置 modem (后端 501) */
export function resetModem(deviceId: string) {
  return getClient().post(`/modems/${encodeURIComponent(deviceId)}/reset`)
}
