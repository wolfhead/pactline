import { apiDelete, apiGet, apiPatch, apiPost } from './client'
import type { User } from '@/identity'

export interface DirectoryPrincipal {
  subject_id: string
  name: string
  email: string | null
  avatar_url: string | null
}

export type InvitationStatus = 'pending' | 'accepted' | 'expired' | 'revoked'

export interface Invitation {
  id: string
  target_subject_id: string
  target_snapshot: {
    name?: string
    email?: string | null
    avatar_url?: string | null
  }
  status: InvitationStatus
  expires_at: string
  created_at: string
  updated_at: string
  delivery?: {
    channel: string
    status: 'delivered' | 'failed'
    error_category: string | null
    attempted_at: string
  }
}

export function searchDirectory(query: string): Promise<DirectoryPrincipal[]> {
  return apiGet<DirectoryPrincipal[]>(`/api/admin/directory/search?q=${encodeURIComponent(query)}`)
}

export function listInvitations(): Promise<Invitation[]> {
  return apiGet<Invitation[]>('/api/admin/invitations')
}

export function createInvitation(subjectID: string): Promise<Invitation> {
  return apiPost<Invitation>('/api/admin/invitations', { subject_id: subjectID })
}

export function resendInvitation(id: string): Promise<Invitation> {
  return apiPost<Invitation>(`/api/admin/invitations/${id}/resend`)
}

export function rotateInvitationLink(id: string): Promise<{ url: string }> {
  return apiPost<{ url: string }>(`/api/admin/invitations/${id}/link`)
}

export function revokeInvitation(id: string): Promise<void> {
  return apiDelete<void>(`/api/admin/invitations/${id}`)
}

export function listAdminUsers(): Promise<User[]> {
  return apiGet<User[]>('/api/admin/users')
}

export function setUserActive(id: string, active: boolean): Promise<void> {
  return apiPatch<void>(`/api/admin/users/${id}`, { active })
}

export function startImpersonation(userID: string): Promise<void> {
  return apiPost<void>('/api/admin/impersonation', { user_id: userID })
}
