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

/** 删除离线 modem */
export function deleteModem(deviceId: string) {
  return getClient().delete<{ message: string }>(`/modems/${encodeURIComponent(deviceId)}`)
}

/** 重置 modem (后端 501) */
export function resetModem(deviceId: string) {
  return getClient().post(`/modems/${encodeURIComponent(deviceId)}/reset`)
}

/** 设置 modem 备注名 */
export function setModemNickname(deviceId: string, nickname: string) {
  return getClient().put<ModemRow>(`/modems/${encodeURIComponent(deviceId)}/nickname`, { nickname })
}
