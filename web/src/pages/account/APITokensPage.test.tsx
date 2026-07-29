import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import APITokensPage from './APITokensPage'
import * as accessAPI from '@/api/access'

vi.mock('@/api/access')

const METADATA: accessAPI.APIToken = {
  id: 'token-1',
  user_id: 'user-1',
  name: 'Planning agent',
  display_prefix: 'bb_pat_visible',
  scopes: ['work:write'],
  expires_at: '2027-01-01T00:00:00Z',
  last_used_at: null,
  revoked_at: null,
  revoked_by_user_id: null,
  created_at: '2026-07-29T00:00:00Z',
}
const SECRET = 'bb_pat_once_only.secret-material'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  localStorage.clear()
  sessionStorage.clear()
})

beforeEach(() => {
  vi.mocked(accessAPI.listAPITokens)
    .mockResolvedValueOnce({ items: [] })
    .mockResolvedValue({ items: [METADATA] })
  vi.mocked(accessAPI.listOwnAPIActivity).mockResolvedValue({ items: [] })
  vi.mocked(accessAPI.createAPIToken).mockResolvedValue({ ...METADATA, token: SECRET })
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  })
})

describe('APITokensPage', () => {
  it('shows the issued secret once and removes it completely when closed', async () => {
    render(<APITokensPage />)
    await screen.findByText('还没有 API Token。')

    fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'Planning agent' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    const secretField = await screen.findByLabelText('新 API Token')
    expect(secretField).toHaveValue(SECRET)
    expect(accessAPI.createAPIToken).toHaveBeenCalledWith({
      name: 'Planning agent',
      scopes: ['work:write'],
      expires_in_days: 90,
    })

    fireEvent.click(screen.getByRole('button', { name: '复制' }))
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(SECRET))
    fireEvent.click(screen.getByRole('button', { name: '关闭完整 Token' }))

    expect(screen.queryByDisplayValue(SECRET)).not.toBeInTheDocument()
    expect(screen.getByText('Planning agent')).toBeVisible()
    expect(document.body.textContent).not.toContain(SECRET)
    expect(JSON.stringify(localStorage)).not.toContain(SECRET)
    expect(JSON.stringify(sessionStorage)).not.toContain(SECRET)
    expect(window.location.href).not.toContain(SECRET)
  })
})
