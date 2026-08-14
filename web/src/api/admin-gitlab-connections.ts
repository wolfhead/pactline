import { apiGet, apiPatch, apiPost } from './client'

export interface GitLabConnection {
  id: string
  version: number
  label: string
  origin: string
  gitlab_project_id: number
  path_with_namespace: string
  canonical_web_url: string
  default_branch: string
  credential_expires_at: string | null
  status: 'active' | 'disabled'
  last_validated_at: string
  created_at: string
  updated_at: string
}

export interface CreateGitLabConnectionBody {
  label: string
  repository_url: string
  credential: string
  credential_expires_at?: string | null
}

export function listGitLabConnections(): Promise<GitLabConnection[]> {
  return apiGet<GitLabConnection[]>('/api/admin/gitlab-connections')
}

export function createGitLabConnection(
  body: CreateGitLabConnectionBody,
): Promise<GitLabConnection> {
  return apiPost<GitLabConnection>('/api/admin/gitlab-connections', body)
}

export function rotateGitLabCredential(
  connection: GitLabConnection,
  credential: string,
  credentialExpiresAt?: string | null,
): Promise<GitLabConnection> {
  return apiPatch<GitLabConnection>(
    `/api/admin/gitlab-connections/${connection.id}/credential`,
    {
      version: connection.version,
      credential,
      credential_expires_at: credentialExpiresAt ?? null,
    },
  )
}

export function validateGitLabConnection(connection: GitLabConnection): Promise<GitLabConnection> {
  return apiPost<GitLabConnection>(`/api/admin/gitlab-connections/${connection.id}/validate`, {
    version: connection.version,
  })
}

export function disableGitLabConnection(connection: GitLabConnection): Promise<GitLabConnection> {
  return apiPost<GitLabConnection>(`/api/admin/gitlab-connections/${connection.id}/disable`, {
    version: connection.version,
  })
}
