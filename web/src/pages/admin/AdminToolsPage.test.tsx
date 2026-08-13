import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import AdminToolsPage from './AdminToolsPage'
import * as adminToolsAPI from '@/api/admin-tools'
import type { User } from '@/identity'

vi.mock('@/api/admin-tools')

const RECIPIENT: User = {
  id: 'recipient-1', name: 'Test Recipient', email: 'recipient@example.test', avatar_url: null,
  platform_role: 'MEMBER', access_status: 'APPROVED', roles: [], active: true,
  created_at: '2026-08-06T00:00:00Z', updated_at: '2026-08-06T00:00:00Z',
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('AdminToolsPage', () => {
  it('queues a fixed DM test for an eligible recipient', async () => {
    vi.mocked(adminToolsAPI.listNotificationTestRecipients).mockResolvedValue([RECIPIENT])
    vi.mocked(adminToolsAPI.requestNotificationTest).mockResolvedValue({
      event_id: 'event-1', status: 'queued',
    })

    render(<AdminToolsPage />)

    expect(await screen.findByRole('option', { name: /Test Recipient/ })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '发送测试 DM' }))

    await waitFor(() => expect(adminToolsAPI.requestNotificationTest).toHaveBeenCalledWith(RECIPIENT.id))
    expect(await screen.findByRole('status')).toHaveTextContent('测试事件已提交')
    expect(screen.getByText(/Event ID: event-1/)).toBeInTheDocument()
  })

  it('explains when no Lark recipient is available', async () => {
    vi.mocked(adminToolsAPI.listNotificationTestRecipients).mockResolvedValue([])

    render(<AdminToolsPage />)

    expect(await screen.findByText(/当前没有可以接收测试 DM 的用户/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送测试 DM' })).toBeDisabled()
  })
})
