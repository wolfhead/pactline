import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { MemoryRouter } from 'react-router-dom'
import Mine from './Mine'
import { apiGet, apiPost } from '../../api/client'
import { useIdentity } from '../../identity'
import type { Bounty, Credit, User } from '../../types'

// Mocks both modules so apiGet/apiPost resolution and the current identity
// are fully controllable per test, without touching global fetch, a real
// backend, or IdentityProvider. Mirrors the pattern established in
// src/pages/WorkFeed.test.tsx and src/components/CreditPanel.test.tsx.
vi.mock('../../api/client')
vi.mock('../../identity')

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

const OTHER = '00000000-0000-0000-0000-000000000004'
const SWITCH_USERS: User[] = [
  ...USERS,
  { id: OTHER, name: '研发 E', email: 'e@example.com', roles: ['ENGINEER'], active: true },
]

function makeBounty(overrides: Partial<Bounty> = {}): Bounty {
  return {
    id: 'b-1',
    type: 'DELIVERY',
    title: 'a bounty',
    goal: '',
    acceptance_criteria: '',
    visibility: 'PUBLIC',
    business_lines: [],
    commitment: 'COMMITTED',
    status: 'CLAIMED',
    sponsor_id: ME,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// A thin harness that reassigns the mocked useIdentity() return value on
// every render, so a click can flip the "current identity" the mounted
// <Mine/> observes — the same thing the real header's UserSwitcher does by
// calling switchTo(), without unmounting the page. Mine itself calls
// useIdentity() on every render, so reassigning the mock here (which runs
// before Mine's own render, since this component is Mine's parent) is
// observed by Mine on the very next render. Mirrors src/pages/Board.test.tsx.
function IdentitySwitchHarness() {
  const [meId, setMeId] = useState(ME)
  const me = SWITCH_USERS.find((u) => u.id === meId) ?? null
  mockedUseIdentity.mockReturnValue({ me, users: SWITCH_USERS, switchTo: setMeId })
  return (
    <>
      <button onClick={() => setMeId(OTHER)}>switch-to-other</button>
      <Mine />
    </>
  )
}

function renderSwitchHarness() {
  return render(
    <MemoryRouter>
      <IdentitySwitchHarness />
    </MemoryRouter>,
  )
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
    if (typeof path === 'string' && path === '/api/legacy/credits/pending') {
      return Promise.resolve(pending)
    }
    return Promise.resolve([])
  })
}

// Makes exactly the claimed-bounties request (the second of the three
// concurrent Promise.all calls) reject, while the other two resolve empty.
// Used to prove the failure branch surfaces regardless of which of the
// three concurrent requests is the one that rejects.
function mockGetsWithFailure(errorMessage: string) {
  mockedApiGet.mockImplementation((path: unknown) => {
    if (typeof path === 'string' && path.startsWith('/api/legacy/bounties?claimed_by=')) {
      return Promise.reject(new Error(errorMessage))
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
    expect(mockedApiPost).toHaveBeenCalledWith('/api/legacy/credits/credit-x/respond', { status: 'CONFIRMED' })
  })

  it('shows the loading hint while the three initial requests are pending, and not the empty-state text (B1)', async () => {
    mockIdentity()
    let resolveAll!: (v: unknown[]) => void
    mockedApiGet.mockReturnValue(
      new Promise((resolve) => {
        resolveAll = resolve as (v: unknown[]) => void
      }),
    )

    renderMine()

    expect(screen.getByText('加载中…')).toBeInTheDocument()
    expect(screen.queryByText('没有待确认的署名。')).not.toBeInTheDocument()

    // Resolve so the pending promise doesn't leak into later tests.
    resolveAll([])
    await waitFor(() => expect(screen.queryByText('加载中…')).not.toBeInTheDocument())
  })

  it('shows the empty-state text once the requests resolve with nothing pending, and hides the loading hint (B2)', async () => {
    mockIdentity()
    mockGets([])

    renderMine()

    await waitFor(() => expect(screen.getByText('没有待确认的署名。')).toBeInTheDocument())
    expect(screen.queryByText('加载中…')).not.toBeInTheDocument()
  })

  it('renders the failure message including the underlying error message when one of the three concurrent requests rejects (B3)', async () => {
    mockIdentity()
    mockGetsWithFailure('network down')

    renderMine()

    await waitFor(() => expect(document.querySelector('.error')).not.toBeNull())
    const text = document.querySelector('.error')?.textContent ?? ''
    expect(text).toContain('network down')
    expect(text).not.toContain('[object Object]')
    expect(text).not.toBe('')
  })

  it('re-enables the buttons and keeps the credit in the list when respond() fails (B4)', async () => {
    mockIdentity()
    mockGets([makeCredit({ id: 'credit-y' })])

    renderMine()

    await waitFor(() => expect(screen.getByText('确认')).toBeInTheDocument())

    let rejectPost!: (err: Error) => void
    mockedApiPost.mockReturnValue(
      new Promise((_resolve, reject) => {
        rejectPost = reject
      }),
    )

    const confirmButton = screen.getByText('确认') as HTMLButtonElement
    const declineButton = screen.getByText('拒绝') as HTMLButtonElement

    fireEvent.click(confirmButton)

    await waitFor(() => expect(confirmButton.disabled).toBe(true))
    expect(declineButton.disabled).toBe(true)

    rejectPost(new Error('respond failed'))

    // The failed respond() must re-enable the buttons rather than leaving
    // them stuck disabled forever, and the credit must still be there to
    // retry against — not silently dropped from the list. This is the
    // counterpart to the success-path test above: a respond() that only
    // resets its in-flight marker on success would leave these buttons
    // disabled here.
    await waitFor(() => expect(confirmButton.disabled).toBe(false))
    expect(declineButton.disabled).toBe(false)
    expect(screen.getByText('确认')).toBeInTheDocument()
    expect(screen.getByText('拒绝')).toBeInTheDocument()

    // The failure surfaces inline, near the action, without blanking out
    // the rest of the page's data (cf. the separate load-failure path in B3).
    expect(screen.getByText('响应失败:respond failed')).toBeInTheDocument()
  })
})

describe('Mine — identity switch refetch', () => {
  it("refetches on identity switch and the new identity's data replaces the old — every list on this page is scoped to the current user by construction, so this is the page most directly exposed by a missing identity dependency", async () => {
    mockedApiGet.mockImplementation((path: unknown) => {
      if (typeof path === 'string' && path === `/api/legacy/bounties?claimed_by=${ME}`) {
        return Promise.resolve([makeBounty({ id: 'b-me', title: 'D 认领的单' })])
      }
      if (typeof path === 'string' && path === `/api/legacy/bounties?claimed_by=${OTHER}`) {
        return Promise.resolve([makeBounty({ id: 'b-other', title: 'E 认领的单' })])
      }
      return Promise.resolve([])
    })

    renderSwitchHarness()

    await waitFor(() => expect(screen.getByText('D 认领的单')).toBeInTheDocument())
    // Requests carry the caller's own id, not a stale one, per identity.tsx's
    // synchronous switchTo assignment — the request-count check below proves
    // a second round of fetches actually fired.
    const callsBeforeSwitch = mockedApiGet.mock.calls.length

    fireEvent.click(screen.getByText('switch-to-other'))

    await waitFor(() => expect(screen.getByText('E 认领的单')).toBeInTheDocument())
    expect(screen.queryByText('D 认领的单')).not.toBeInTheDocument()
    expect(mockedApiGet.mock.calls.length).toBeGreaterThan(callsBeforeSwitch)
    expect(mockedApiGet).toHaveBeenCalledWith(`/api/legacy/bounties?claimed_by=${OTHER}`)
  })

  it("does not show the previous identity's rows while the new identity's request is still in flight", async () => {
    mockedApiGet.mockImplementation((path: unknown) => {
      if (typeof path === 'string' && path === `/api/legacy/bounties?claimed_by=${ME}`) {
        return Promise.resolve([makeBounty({ id: 'b-me', title: 'D 认领的单' })])
      }
      return Promise.resolve([])
    })

    renderSwitchHarness()

    await waitFor(() => expect(screen.getByText('D 认领的单')).toBeInTheDocument())

    let resolveOther!: (bounties: Bounty[]) => void
    mockedApiGet.mockImplementation((path: unknown) => {
      if (typeof path === 'string' && path === `/api/legacy/bounties?claimed_by=${OTHER}`) {
        return new Promise<Bounty[]>((resolve) => {
          resolveOther = resolve
        })
      }
      // The other two concurrent requests (pending credits, sponsored
      // bounties) resolve immediately; only claimed_by is held open, so
      // Promise.all stays pending on that one alone.
      return Promise.resolve([])
    })

    fireEvent.click(screen.getByText('switch-to-other'))

    // Before the new identity's requests resolve, the previous identity's
    // row must already be gone — replaced by the loading state, not left on
    // screen for a user with no right to see it.
    await waitFor(() => expect(screen.getAllByText('加载中…').length).toBeGreaterThan(0))
    expect(screen.queryByText('D 认领的单')).not.toBeInTheDocument()

    resolveOther([makeBounty({ id: 'b-other', title: 'E 认领的单' })])
    await waitFor(() => expect(screen.getByText('E 认领的单')).toBeInTheDocument())
  })
})
