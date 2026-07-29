import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import LoginPage from './LoginPage'
import { useIdentity } from '@/identity'

vi.mock('@/identity', () => ({ useIdentity: vi.fn() }))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('LoginPage', () => {
  it('returns an authenticated user to the protected deep link', async () => {
    vi.mocked(useIdentity).mockReturnValue({
      status: 'authenticated',
      actor: null,
      subject: null,
      me: null,
      users: [],
      impersonation: null,
      isReadOnly: false,
      error: null,
      refresh: vi.fn(),
      loginForDevelopment: vi.fn(),
      logout: vi.fn(),
      endImpersonation: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={[{ pathname: '/login', state: { from: '/tasks/42' } }]}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/tasks/42" element={<p>Task deep link</p>} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByText('Task deep link')).toBeVisible()
  })
})
