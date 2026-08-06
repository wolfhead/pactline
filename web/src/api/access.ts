import { apiDelete, apiGet, apiPost } from './client'
import type { User } from '@/identity'

export type APITokenScope = 'work:read' | 'work:execute' | 'work:write'

export interface APIToken {
  id: string
  user_id: string
  name: string
  display_prefix: string
  scopes: APITokenScope[]
  expires_at: string
  last_used_at: string | null
  revoked_at: string | null
  revoked_by_user_id: string | null
  created_at: string
}

export interface IssuedAPIToken extends APIToken {
  token: string
}

export interface APIAccessEvent {
  id: string
  occurred_at: string
  request_id: string
  auth_method: 'session' | 'api_token' | 'unknown'
  auth_outcome: 'authenticated' | 'rejected'
  user_id?: string
  token_id?: string
  token_name?: string
  method: string
  route_pattern: string
  status_code: number
  problem_code?: string
  duration_ms: number
  response_bytes: number
  idempotency_replayed: boolean
  user_agent: string
  network_address?: string
}

export interface APIAccessPage {
  items: APIAccessEvent[]
  next_cursor?: string
}

export type LarkAPIOutcome =
  | 'succeeded'
  | 'rejected'
  | 'rate_limited'
  | 'unavailable'
  | 'cancelled'
  | 'contract_error'

export interface LarkAPIAuditEvent {
  id: string
  occurred_at: string
  operation: string
  category: string
  method: string
  route_pattern: string
  credential_kind: 'none' | 'app' | 'tenant' | 'user'
  outcome: LarkAPIOutcome
  http_status?: number
  provider_code?: number
  provider_request_id?: string
  error_category?: string
  duration_ms: number
  request_bytes: number
  response_bytes: number
  request_id?: string
  actor_user_id?: string
  subject_user_id?: string
  agent_run_id?: string
  application_event_id?: string
}

export interface LarkAPIAuditPage {
  items: LarkAPIAuditEvent[]
  next_cursor?: string
}

export interface LarkAPIAuditFilters {
  operation?: string
  category?: string
  outcome?: LarkAPIOutcome
  status?: number
  providerRequestID?: string
  requestID?: string
  actorUserID?: string
  agentRunID?: string
  eventID?: string
  from?: string
  to?: string
  cursor?: string
  pageSize?: number
}

export interface AdminAPIToken {
  token: APIToken
  user: User
}

export interface APIAccessFilters {
  userID?: string
  tokenID?: string
  method?: string
  route?: string
  status?: number
  requestID?: string
  importantOnly?: boolean
  from?: string
  to?: string
  cursor?: string
  pageSize?: number
}

export function listAPITokens(): Promise<{ items: APIToken[] }> {
  return apiGet('/api/account/tokens')
}

export function createAPIToken(request: {
  name: string
  scopes: APITokenScope[]
  expires_in_days: 30 | 90 | 365
}): Promise<IssuedAPIToken> {
  return apiPost('/api/account/tokens', request)
}

export function revokeAPIToken(id: string): Promise<void> {
  return apiDelete(`/api/account/tokens/${encodeURIComponent(id)}`)
}

export function listOwnAPIActivity(filters: Omit<APIAccessFilters, 'userID'> = {}): Promise<APIAccessPage> {
  return apiGet(`/api/account/api-activity?${accessQuery(filters)}`)
}

export function listAdminAPITokens(): Promise<{ items: AdminAPIToken[] }> {
  return apiGet('/api/admin/api-tokens')
}

export function revokeAdminAPIToken(id: string): Promise<void> {
  return apiDelete(`/api/admin/api-tokens/${encodeURIComponent(id)}`)
}

export function listAdminAPIActivity(filters: APIAccessFilters = {}): Promise<APIAccessPage> {
  return apiGet(`/api/admin/api-activity?${accessQuery(filters)}`)
}

export function listAdminLarkAPIActivity(
  filters: LarkAPIAuditFilters = {},
): Promise<LarkAPIAuditPage> {
  const query = new URLSearchParams()
  if (filters.operation) query.set('operation', filters.operation)
  if (filters.category) query.set('category', filters.category)
  if (filters.outcome) query.set('outcome', filters.outcome)
  if (filters.status !== undefined) query.set('status', String(filters.status))
  if (filters.providerRequestID) query.set('provider_request_id', filters.providerRequestID)
  if (filters.requestID) query.set('request_id', filters.requestID)
  if (filters.actorUserID) query.set('actor_user_id', filters.actorUserID)
  if (filters.agentRunID) query.set('agent_run_id', filters.agentRunID)
  if (filters.eventID) query.set('event_id', filters.eventID)
  if (filters.from) query.set('from', filters.from)
  if (filters.to) query.set('to', filters.to)
  if (filters.cursor) query.set('cursor', filters.cursor)
  query.set('page_size', String(filters.pageSize ?? 50))
  return apiGet(`/api/admin/lark-api-activity?${query.toString()}`)
}

function accessQuery(filters: APIAccessFilters): string {
  const query = new URLSearchParams()
  if (filters.userID) query.set('user_id', filters.userID)
  if (filters.tokenID) query.set('token_id', filters.tokenID)
  if (filters.method) query.set('method', filters.method)
  if (filters.route) query.set('route', filters.route)
  if (filters.status !== undefined) query.set('status', String(filters.status))
  if (filters.requestID) query.set('request_id', filters.requestID)
  if (filters.importantOnly) query.set('important_only', 'true')
  if (filters.from) query.set('from', filters.from)
  if (filters.to) query.set('to', filters.to)
  if (filters.cursor) query.set('cursor', filters.cursor)
  query.set('page_size', String(filters.pageSize ?? 50))
  return query.toString()
}
