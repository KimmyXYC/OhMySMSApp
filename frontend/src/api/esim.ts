import { getClient } from './client'
import type {
  ESimCard,
  ESimCardDetail,
  ESimProfile,
  ListResponse,
} from '@/types/api'

// ─── eSIM Cards ───

/** 获取所有 eSIM 卡 */
export function listCards() {
  return getClient().get<ListResponse<ESimCard>>('/esim/cards')
}

/** 获取单张 eSIM 卡详情（含 profiles） */
export function getCard(id: number) {
  return getClient().get<ESimCardDetail>(`/esim/cards/${id}`)
}

/** 获取卡的 profile 列表 */
export function listCardProfiles(cardId: number) {
  return getClient().get<ListResponse<ESimProfile>>(`/esim/cards/${cardId}/profiles`)
}

/** 触发后台 lpac 发现/刷新 */
export function discoverCard(cardId: number) {
  return getClient().post<{ message: string }>(`/esim/cards/${cardId}/discover`)
}

/** 修改卡备注名（存 DB） */
export function setCardNickname(cardId: number, nickname: string) {
  return getClient().put<ESimCard>(`/esim/cards/${cardId}/nickname`, { nickname })
}

// ─── eSIM Profiles ───

/** 启用 profile（后端 202，异步生效） */
export function enableProfile(iccid: string) {
  return getClient().post<{ message: string }>(`/esim/profiles/${encodeURIComponent(iccid)}/enable`)
}

/** 禁用 profile（后端 202，异步生效） */
export function disableProfile(iccid: string) {
  return getClient().post<{ message: string }>(`/esim/profiles/${encodeURIComponent(iccid)}/disable`)
}

/** 修改 profile 昵称（写入 eUICC 卡片） */
export function setProfileNickname(iccid: string, nickname: string) {
  return getClient().put<ESimProfile>(
    `/esim/profiles/${encodeURIComponent(iccid)}/nickname`,
    { nickname },
  )
}
