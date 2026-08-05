import { apiGet, apiPatch, apiPost } from './client'
import type { AccessStatus, User } from '@/identity'

export function listAdminUsers(): Promise<User[]> {
  return apiGet<User[]>('/api/admin/users')
}

export function setUserActive(id: string, active: boolean): Promise<void> {
  return apiPatch<void>(`/api/admin/users/${id}`, { active })
}

export function setUserAccessStatus(id: string, accessStatus: AccessStatus): Promise<void> {
  return apiPatch<void>(`/api/admin/users/${id}`, { access_status: accessStatus })
}

export function startImpersonation(userID: string): Promise<void> {
  return apiPost<void>('/api/admin/impersonation', { user_id: userID })
}
