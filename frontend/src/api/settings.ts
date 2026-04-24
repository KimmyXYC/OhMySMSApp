import { getClient } from './client'
import type { TelegramSettings, TelegramPutRequest } from '@/types/api'

/** 获取 Telegram 设置 */
export function getTelegramSettings() {
  return getClient().get<TelegramSettings>('/settings/telegram')
}

/** 更新 Telegram 设置 */
export function putTelegramSettings(data: TelegramPutRequest) {
  return getClient().put<TelegramSettings>('/settings/telegram', data)
}
