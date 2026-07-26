import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Mine from './Mine'
import { apiGet, apiPost } from '../api/client'
import { useIdentity } from '../identity'
import type { Credit, User } from '../types'

// Mocks both modules so apiGet/apiPost resolution and the current identity
// are fully controllable per test, without touching global fetch, a real
// backend, or IdentityProvider. Mirrors the pattern established in
// src/pages/WorkFeed.test.tsx and src/components/CreditPanel.test.tsx.
vi.mock('../api/client')
vi.mock('../identity')

const mockedApiGet = vi.mocked(apiGet)
const mockedApiPost = vi.mocked(apiPost)
const mockedUseIdentity = vi.mocked(useIdentity)

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this, a
// component rendered by one test stays mounted and pollutes the next test's
// queries. Mirrors src/pages/WorkFeed.test.tsx.
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const ME = '00000000-0000-0000-0000-000000000003'

const USERS: User[] = [{ id: ME, name: '研发 D', email: 'd@example.com', roles: ['ENGINEER'], active: true }]

function mockIdentity() {
  mockedUseIdentity.mockReturnValue({ me: USERS[0], users: USERS, switchTo: vi.fn() })
}

function makeCredit(overrides: Partial<Credit> = {}): Credit {
  return {
    id: 'credit-1',
    bounty_id: 'b-1',
    user_id: ME,
    role: 'REVIEW',
    status: 'PENDING',
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// Only /api/credits/pending is varied per test; the claimed/sponsored
// bounty lists are irrelevant to this behaviour, so they always resolve
// empty.
function mockGets(pending: Credit[]) {
  mockedApiGet.mockImplementation((path: unknown) => {
    if (typeof path === 'string' && path === '/api/credits/pending') {
      return Promise.resolve(pending)
    }
    return Promise.resolve([])
  })
}

function renderMine() {
  return render(
    <MemoryRouter>
      <Mine />
    </MemoryRouter>,
  )
}

describe('Mine', () => {
  it('removes a credit from the pending list after a successful respond, and disables its buttons while the request is in flight', async () => {
    mockIdentity()
    mockGets([makeCredit({ id: 'credit-x' })])

    renderMine()

    await waitFor(() => expect(screen.getByText('确认')).toBeInTheDocument())

    let resolvePost!: () => void
    mockedApiPost.mockReturnValue(
      new Promise((resolve) => {
        resolvePost = () => resolve(undefined)
      }),
    )

    const confirmButton = screen.getByText('确认') as HTMLButtonElement
    const declineButton = screen.getByText('拒绝') as HTMLButtonElement
    expect(confirmButton.disabled).toBe(false)
    expect(declineButton.disabled).toBe(false)

    fireEvent.click(confirmButton)

    await waitFor(() => expect(confirmButton.disabled).toBe(true))
    expect(declineButton.disabled).toBe(true)

    // Once respond() resolves, Mine reloads the pending list — mock the
    // reload to come back with the credit gone (as the backend would after
    // a successful CONFIRMED response).
    mockGets([])
    resolvePost()

    await waitFor(() => expect(screen.queryByText('确认')).not.toBeInTheDocument())
    expect(screen.getByText('没有待确认的署名。')).toBeInTheDocument()
    expect(mockedApiPost).toHaveBeenCalledWith('/api/credits/credit-x/respond', { status: 'CONFIRMED' })
  })
})
