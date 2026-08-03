import { etagForVersion, requireVersioned, v1Get, v1Patch } from './v1/client'

export interface AgentConversationProject {
  id: string
  number: number
  name: string
}

export interface AgentConversation {
  id: string
  provider: 'lark'
  external_id: string
  name: string
  enabled: boolean
  binding_active: boolean
  default_project?: AgentConversationProject
  business_context: string
  version: number
  can_manage: boolean
  created_by: string
  updated_by: string
  last_seen_at: string
  created_at: string
  updated_at: string
}

export interface AgentConversationPatch {
  enabled?: boolean
  binding_active?: boolean
  default_project_number?: number
  business_context?: string
}

export function listAgentConversations(): Promise<AgentConversation[]> {
  return v1Get<{ items: AgentConversation[] }>('/api/v1/agent-conversations')
    .then(({ value }) => value.items)
}

export function updateAgentConversation(
  id: string,
  version: number,
  body: AgentConversationPatch,
): Promise<AgentConversation> {
  return v1Patch<AgentConversation>(`/api/v1/agent-conversations/${id}`, {
    ifMatch: etagForVersion(version),
    body,
  }).then((response) => requireVersioned(response).value)
}

export function agentConversationLabel(conversation: AgentConversation): string {
  const name = conversation.name.trim()
  if (name) return name
  const suffix = conversation.external_id.slice(-8)
  return `Lark 群 · ${suffix || conversation.external_id}`
}
