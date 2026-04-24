import { getClient } from './client'
import type { SimRow, ListResponse } from '@/types/api'

/** 获取所有 eSIM SIM（card_type 含 esim） */
export async function listESimSims() {
  // 后端没有专门的 eSIM 端点，复用 /api/sims 然后前端过滤
  const { data } = await getClient().get<ListResponse<SimRow>>('/sims')
  return data.items.filter(
    (s) => s.card_type === 'sticker_esim' || s.card_type === 'embedded_esim',
  )
}
