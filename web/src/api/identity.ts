import { apiDelete, apiGet, apiPost } from './client'
import type { MeResponse } from '@/identity'

export function getMe(): Promise<MeResponse> {
  return apiGet<MeResponse>('/api/me')
}

export function logout(): Promise<void> {
  return apiPost<void>('/api/auth/logout')
}

export function createDevelopmentSession(userID: string): Promise<void> {
  return apiPost<void>('/api/auth/dev/session', { user_id: userID })
}

export function endImpersonation(): Promise<void> {
  return apiDelete<void>('/api/admin/impersonation')
}
