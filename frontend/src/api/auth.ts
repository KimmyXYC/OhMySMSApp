import client from './client'
import type { LoginRequest, LoginResponse } from '@/types/api'

export function login(data: LoginRequest) {
  return client.post<LoginResponse>('/auth/login', data)
}

export function logout() {
  return client.post('/auth/logout')
}

/** 验证当前 token 有效性（可选端点） */
export function checkAuth() {
  return client.get('/auth/me')
}
