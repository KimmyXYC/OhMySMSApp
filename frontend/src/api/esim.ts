import client from './client'
import type { ESimCard, ESimProfileAction, ESimSetNickname, Sim } from '@/types/api'

/** 获取所有 eSIM 卡 */
export function listESimCards() {
  return client.get<ESimCard[]>('/esim/cards')
}

/** 获取某 eSIM 卡的 profile 列表 */
export function listESimProfiles(cardId: number) {
  return client.get<Sim[]>(`/esim/cards/${cardId}/profiles`)
}

/** 启用/禁用 eSIM profile */
export function toggleESimProfile(data: ESimProfileAction) {
  return client.post('/esim/profile/toggle', data)
}

/** 设置 eSIM profile nickname */
export function setESimNickname(data: ESimSetNickname) {
  return client.post('/esim/profile/nickname', data)
}

/** 刷新 eSIM 卡信息（触发 lpac 扫描） */
export function refreshESimCard(cardId: number) {
  return client.post(`/esim/cards/${cardId}/refresh`)
}
