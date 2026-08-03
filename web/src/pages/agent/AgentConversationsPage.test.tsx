import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import AgentConversationsPage from './AgentConversationsPage'
import * as conversationsApi from '@/api/agent-conversations'
import * as projectsApi from '@/api/projects'

vi.mock('@/api/agent-conversations')
vi.mock('@/api/projects')
vi.mock('@/identity', () => ({
  useIdentity: () => ({ me: { id: 'admin' }, isReadOnly: false }),
}))

const CONVERSATION: conversationsApi.AgentConversation = {
  id: 'conversation-1',
  provider: 'lark',
  external_id: 'oc_release_room',
  name: '模型发布讨论群',
  enabled: true,
  binding_active: true,
  default_project: { id: 'project-7', number: 7, name: '模型平台' },
  business_context: '这里讨论模型上线与预发验证。',
  version: 3,
  can_manage: true,
  created_by: 'admin',
  updated_by: 'admin',
  last_seen_at: '2026-08-03T08:00:00Z',
  created_at: '2026-08-01T08:00:00Z',
  updated_at: '2026-08-03T08:00:00Z',
}

describe('AgentConversationsPage', () => {
  beforeEach(() => {
    vi.mocked(conversationsApi.agentConversationLabel)
      .mockImplementation((conversation) => conversation.name || 'Lark 群')
    vi.mocked(conversationsApi.listAgentConversations).mockResolvedValue([CONVERSATION])
    vi.mocked(projectsApi.listProjects).mockResolvedValue([{
      id: 'project-7', number: 7, version: 1, name: '模型平台', description: '',
      creator: { id: 'admin', name: 'Admin', email: null }, archived_at: null,
      created_at: '', updated_at: '', completed_tasks: 0, eligible_tasks: 0,
    }])
    vi.mocked(conversationsApi.updateAgentConversation).mockResolvedValue({
      ...CONVERSATION,
      business_context: '新的稳定背景',
      version: 4,
    })
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('shows the resolved default Project and saves bounded business context', async () => {
    render(<MemoryRouter><AgentConversationsPage /></MemoryRouter>)

    expect(await screen.findByRole('heading', { name: '模型发布讨论群' })).toBeVisible()
    expect(screen.getByRole('combobox', { name: '项目' })).toHaveValue('7')
    const context = screen.getByRole('textbox', { name: /群业务背景/ })
    expect(context).toHaveValue('这里讨论模型上线与预发验证。')

    fireEvent.change(context, { target: { value: '新的稳定背景' } })
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => expect(conversationsApi.updateAgentConversation).toHaveBeenCalledWith(
      'conversation-1',
      3,
      {
        enabled: true,
        binding_active: true,
        business_context: '新的稳定背景',
        default_project_number: 7,
      },
    ))
    expect(await screen.findByText('已保存')).toBeVisible()
  })
})
