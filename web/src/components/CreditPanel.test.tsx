import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import CreditPanel from './CreditPanel'
import { apiGet, apiPost } from '../api/client'
import { useIdentity } from '../identity'
import type { Bounty, Credit, User } from '../types'

// Mocks both modules so apiGet/apiPost resolution and the current identity
// are fully controllable per test, without touching global fetch, a real
// backend, or IdentityProvider. Mirrors the pattern established in
// src/pages/WorkFeed.test.tsx and src/components/NewBountyForm.test.tsx.
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

const BOUNTY_ID = '00000000-0000-0000-0000-0000000000b1'
const CLAIMER = '00000000-0000-0000-0000-000000000002'
const NOMINEE = '00000000-0000-0000-0000-000000000003'
const STEWARD = '00000000-0000-0000-0000-000000000004'
const SPONSOR = '00000000-0000-0000-0000-000000000005'

const USERS: User[] = [
  { id: CLAIMER, name: '研发 C', email: 'c@example.com', roles: ['ENGINEER'], active: true },
  { id: NOMINEE, name: '研发 D', email: 'd@example.com', roles: ['ENGINEER'], active: true },
  { id: STEWARD, name: 'Steward F', email: 'f@example.com', roles: ['STEWARD'], active: true },
  { id: SPONSOR, name: '产品 A', email: 'a@example.com', roles: ['SPONSOR'], active: true },
]

function makeBounty(overrides: Partial<Bounty> = {}): Bounty {
  return {
    id: BOUNTY_ID,
    type: 'DELIVERY',
    title: '竞价链路降延迟',
    goal: '',
    acceptance_criteria: '',
    visibility: 'PUBLIC',
    business_lines: [],
    commitment: 'COMMITTED',
    status: 'CLAIMED',
    sponsor_id: SPONSOR,
    claimed_by: CLAIMER,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function makeCredit(overrides: Partial<Credit> = {}): Credit {
  return {
    id: 'credit-1',
    bounty_id: BOUNTY_ID,
    user_id: NOMINEE,
    role: 'SUPPORT',
    status: 'PENDING',
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function mockIdentity(meId: string | null) {
  const me = meId == null ? null : (USERS.find((u) => u.id === meId) ?? null)
  mockedUseIdentity.mockReturnValue({ me, users: USERS, switchTo: vi.fn() })
}

function renderPanel(bounty: Bounty = makeBounty()) {
  return render(<CreditPanel bounty={bounty} onChanged={vi.fn()} />)
}

describe('CreditPanel', () => {
  it("shows confirm and decline controls on the current user's own pending credit", async () => {
    mockIdentity(NOMINEE)
    mockedApiGet.mockResolvedValue([makeCredit({ user_id: NOMINEE, status: 'PENDING' })])

    renderPanel()

    await waitFor(() => expect(screen.getByText('确认')).toBeInTheDocument())
    expect(screen.getByText('拒绝')).toBeInTheDocument()
  })

  it('shows no confirm or decline control when a steward views the same pending credit (no confirming on someone else\'s behalf)', async () => {
    mockIdentity(STEWARD)
    mockedApiGet.mockResolvedValue([makeCredit({ user_id: NOMINEE, status: 'PENDING' })])

    renderPanel()

    await waitFor(() => expect(screen.getByText('研发 D')).toBeInTheDocument())
    expect(screen.queryByText('确认')).not.toBeInTheDocument()
    expect(screen.queryByText('拒绝')).not.toBeInTheDocument()
  })

  it('shows no confirm or decline control for an ordinary user viewing someone else\'s credit', async () => {
    mockIdentity(CLAIMER)
    mockedApiGet.mockResolvedValue([makeCredit({ user_id: NOMINEE, status: 'PENDING' })])

    renderPanel()

    await waitFor(() => expect(screen.getByText('研发 D')).toBeInTheDocument())
    expect(screen.queryByText('确认')).not.toBeInTheDocument()
    expect(screen.queryByText('拒绝')).not.toBeInTheDocument()
  })

  it('shows no confirm or decline control for an already-confirmed credit, even for its owner', async () => {
    mockIdentity(NOMINEE)
    mockedApiGet.mockResolvedValue([makeCredit({ user_id: NOMINEE, status: 'CONFIRMED' })])

    renderPanel()

    await waitFor(() => expect(screen.getByText('已确认')).toBeInTheDocument())
    expect(screen.queryByText('确认')).not.toBeInTheDocument()
    expect(screen.queryByText('拒绝')).not.toBeInTheDocument()
  })

  it('hides nomination controls for a user who is neither the claimer nor a steward', async () => {
    mockIdentity(SPONSOR)
    mockedApiGet.mockResolvedValue([])

    renderPanel()

    await waitFor(() => expect(mockedApiGet).toHaveBeenCalled())
    expect(screen.queryByText('选择成员…')).not.toBeInTheDocument()
    expect(screen.queryByText('提名')).not.toBeInTheDocument()
  })

  it('shows nomination controls for the bounty claimer', async () => {
    mockIdentity(CLAIMER)
    mockedApiGet.mockResolvedValue([])

    renderPanel()

    await waitFor(() => expect(screen.getByText('提名')).toBeInTheDocument())
    expect(screen.getByText('选择成员…')).toBeInTheDocument()
  })

  it('shows nomination controls for a steward, even though they did not claim the bounty', async () => {
    mockIdentity(STEWARD)
    mockedApiGet.mockResolvedValue([])

    renderPanel()

    await waitFor(() => expect(screen.getByText('提名')).toBeInTheDocument())
  })

  it('reveals the evidence input only when the REVIEW role is selected', async () => {
    mockIdentity(CLAIMER)
    mockedApiGet.mockResolvedValue([])

    const { container } = renderPanel()

    await waitFor(() => expect(screen.getByText('提名')).toBeInTheDocument())

    const roleSelect = container.querySelectorAll('select')[1]
    expect(roleSelect).toBeDefined()

    expect(screen.queryByPlaceholderText('评审意见链接(REVIEW 必填)')).not.toBeInTheDocument()

    fireEvent.change(roleSelect, { target: { value: 'REVIEW' } })
    expect(screen.getByPlaceholderText('评审意见链接(REVIEW 必填)')).toBeInTheDocument()

    fireEvent.change(roleSelect, { target: { value: 'SUPPORT' } })
    expect(screen.queryByPlaceholderText('评审意见链接(REVIEW 必填)')).not.toBeInTheDocument()
  })

  it('labels a credit with a nil nominated_by as system-nominated', async () => {
    mockIdentity(CLAIMER)
    mockedApiGet.mockResolvedValue([makeCredit({ user_id: NOMINEE, nominated_by: undefined })])

    renderPanel()

    await waitFor(() => expect(screen.getByText('系统提名')).toBeInTheDocument())
  })

  it('sends a respond call with CONFIRMED when the owner clicks confirm', async () => {
    mockIdentity(NOMINEE)
    mockedApiGet.mockResolvedValue([makeCredit({ id: 'credit-x', user_id: NOMINEE, status: 'PENDING' })])
    mockedApiPost.mockResolvedValue(undefined)

    renderPanel()

    await waitFor(() => expect(screen.getByText('确认')).toBeInTheDocument())
    fireEvent.click(screen.getByText('确认'))

    await waitFor(() => expect(mockedApiPost).toHaveBeenCalledWith('/api/credits/credit-x/respond', { status: 'CONFIRMED' }))
  })
})
