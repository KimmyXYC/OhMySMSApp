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
