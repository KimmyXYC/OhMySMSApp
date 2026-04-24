import { getClient } from './client'
import type { LoginRequest, LoginResponse } from '@/types/api'

export function login(data: LoginRequest) {
  return getClient().post<LoginResponse>('/auth/login', data)
}

export function logout() {
  return getClient().post('/auth/logout')
}

export function checkAuth() {
  return getClient().get<{ username: string }>('/auth/me')
}

/** 修改密码 */
export function changePassword(currentPassword: string, newPassword: string) {
  return getClient().post('/auth/password', {
    current_password: currentPassword,
    new_password: newPassword,
  })
}
