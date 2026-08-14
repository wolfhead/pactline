import { apiGet, apiPatch, apiPost } from "./client";

export type RepositoryProvider = "gitlab" | "github";

export interface RepositoryConnection {
  id: string;
  version: number;
  label: string;
  origin: string;
  provider: RepositoryProvider;
  provider_repository_id: string;
  path_with_namespace: string;
  canonical_web_url: string;
  default_branch: string;
  credential_expires_at: string | null;
  status: "active" | "disabled";
  last_validated_at: string;
  created_at: string;
  updated_at: string;
}

export interface CreateRepositoryConnectionBody {
  label: string;
  provider: RepositoryProvider;
  repository_url: string;
  credential: string;
  credential_expires_at?: string | null;
}

export function listRepositoryConnections(): Promise<RepositoryConnection[]> {
  return apiGet<RepositoryConnection[]>("/api/admin/repository-connections");
}

export function createRepositoryConnection(body: CreateRepositoryConnectionBody): Promise<RepositoryConnection> {
  return apiPost<RepositoryConnection>("/api/admin/repository-connections", body);
}

export function rotateRepositoryCredential(connection: RepositoryConnection, credential: string, credentialExpiresAt?: string | null): Promise<RepositoryConnection> {
  return apiPatch<RepositoryConnection>(`/api/admin/repository-connections/${connection.id}/credential`, {
    version: connection.version,
    credential,
    credential_expires_at: credentialExpiresAt ?? null,
  });
}

export function validateRepositoryConnection(connection: RepositoryConnection): Promise<RepositoryConnection> {
  return apiPost<RepositoryConnection>(`/api/admin/repository-connections/${connection.id}/validate`, {
    version: connection.version,
  });
}

export function disableRepositoryConnection(connection: RepositoryConnection): Promise<RepositoryConnection> {
  return apiPost<RepositoryConnection>(`/api/admin/repository-connections/${connection.id}/disable`, {
    version: connection.version,
  });
}
