import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import BountyDetail from './BountyDetail'
import { apiGet, apiPost } from '../../api/client'
import { useIdentity } from '../../identity'
import type { Bounty, User } from '../../types'

// Mocks all three modules so apiGet/apiPost resolution/rejection and the
// current identity are fully controllable per test, without touching global
// fetch, a real backend, or IdentityProvider. Mirrors the pattern
// established in src/pages/Board.test.tsx and src/pages/Portfolio.test.tsx.
vi.mock('../../api/client')
vi.mock('../../identity')

const mockedApiGet = vi.mocked(apiGet)
const mockedApiPost = vi.mocked(apiPost)
const mockedUseIdentity = vi.mocked(useIdentity)

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this, a
// component rendered by one test stays mounted and pollutes the next test's
// queries. Mirrors src/pages/WorkFeed.test.tsx and src/identity.test.tsx.
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const BOUNTY_ID = '00000000-0000-0000-0000-0000000000b1'
const SPONSOR = '00000000-0000-0000-0000-000000000001'
const TECH_LEAD = '00000000-0000-0000-0000-000000000002'
const STEWARD = '00000000-0000-0000-0000-000000000003'
const ENGINEER = '00000000-0000-0000-0000-000000000004'

const SPONSOR_USER: User = { id: SPONSOR, name: '产品 A', email: 'a@example.com', roles: ['SPONSOR'], active: true }
const TECH_LEAD_USER: User = { id: TECH_LEAD, name: '技术 Leader B', email: 'b@example.com', roles: ['TECH_LEAD'], active: true }
const STEWARD_USER: User = { id: STEWARD, name: 'Steward F', email: 'f@example.com', roles: ['STEWARD'], active: true }
const ENGINEER_USER: User = { id: ENGINEER, name: '研发 C', email: 'c@example.com', roles: ['ENGINEER'], active: true }
const ALL_USERS: User[] = [SPONSOR_USER, TECH_LEAD_USER, STEWARD_USER, ENGINEER_USER]

// Sets the mocked identity for the page under test. `null` matches the real
// IdentityContext's default (me: null) — the behaviour every pre-existing
// test in this file already exercised before this suite started mocking
// ../identity at all.
function mockIdentity(me: User | null) {
  mockedUseIdentity.mockReturnValue({ me, users: ALL_USERS, switchTo: vi.fn() })
}

// Every pre-existing test in this file predates level-setting and ran with
// no identity at all (the real context's default). Default to that here so
// none of them have to be touched individually; tests that care about a
// specific identity call mockIdentity(...) themselves afterwards.
beforeEach(() => {
  mockIdentity(null)
})

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
    status: 'DRAFT',
    sponsor_id: SPONSOR,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// BountyDetail renders CreditPanel (and, for a steward on a settled bounty,
// CalibrationPanel), each of which issues its own apiGet call — for
// `/api/legacy/bounties/:id/credits` and `/api/legacy/bounties/:id/calibrations`
// respectively. Route all three through one mock so the page can render
// fully without a real backend.
function mockGetsFor(bounty: Bounty) {
  mockedApiGet.mockImplementation((path: unknown) => {
    if (typeof path === 'string' && (path.endsWith('/credits') || path.endsWith('/calibrations'))) {
      return Promise.resolve([])
    }
    return Promise.resolve(bounty)
  })
}

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={[`/legacy/bounties/${BOUNTY_ID}`]}>
      <Routes>
        <Route path="/legacy/bounties/:id" element={<BountyDetail />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('BountyDetail', () => {
  it('sends neither retrospective nor person_days when moving to OPEN, even with stale text in both fields (B5)', async () => {
    mockGetsFor(makeBounty({ status: 'DRAFT' }))
    mockedApiPost.mockResolvedValue(makeBounty({ status: 'OPEN' }))

    renderDetail()

    await waitFor(() => expect(screen.getByText('转为「可认领」')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('实际人天'), { target: { value: '3.5' } })
    fireEvent.change(screen.getByLabelText(/结论 \/ 复盘/), { target: { value: '不小心留下的复盘文字' } })

    fireEvent.click(screen.getByText('转为「可认领」'))

    await waitFor(() =>
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/legacy/bounties/${BOUNTY_ID}/transition`, {
        to: 'OPEN',
        retrospective: undefined,
        person_days: undefined,
      }),
    )
  })

  it('sends the retrospective when abandoning (B6)', async () => {
    mockGetsFor(makeBounty({ status: 'DRAFT' }))
    mockedApiPost.mockResolvedValue(makeBounty({ status: 'ABANDONED' }))

    renderDetail()

    await waitFor(() => expect(screen.getByText('转为「已放弃」')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText(/结论 \/ 复盘/), { target: { value: '需求撤回,放弃此单' } })

    fireEvent.click(screen.getByText('转为「已放弃」'))

    await waitFor(() =>
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/legacy/bounties/${BOUNTY_ID}/transition`, {
        to: 'ABANDONED',
        retrospective: '需求撤回,放弃此单',
        person_days: undefined,
      }),
    )
  })

  it('sends person_days when delivering (B6)', async () => {
    mockGetsFor(makeBounty({ status: 'CLAIMED' }))
    mockedApiPost.mockResolvedValue(makeBounty({ status: 'DELIVERED' }))

    renderDetail()

    await waitFor(() => expect(screen.getByText('转为「待验收」')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('实际人天'), { target: { value: '3.5' } })

    fireEvent.click(screen.getByText('转为「待验收」'))

    await waitFor(() =>
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/legacy/bounties/${BOUNTY_ID}/transition`, {
        to: 'DELIVERED',
        retrospective: undefined,
        person_days: 3.5,
      }),
    )
  })

  it('blocks the transition and shows an error for a non-numeric person-days value, issuing no request (B7)', async () => {
    mockGetsFor(makeBounty({ status: 'CLAIMED' }))

    renderDetail()

    await waitFor(() => expect(screen.getByText('转为「待验收」')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('实际人天'), { target: { value: 'abc' } })

    fireEvent.click(screen.getByText('转为「待验收」'))

    expect(await screen.findByText(/abc/)).toBeInTheDocument()
    expect(mockedApiPost).not.toHaveBeenCalled()
  })

  it('submits a zero person-days value instead of dropping it (B8)', async () => {
    mockGetsFor(makeBounty({ status: 'CLAIMED' }))
    mockedApiPost.mockResolvedValue(makeBounty({ status: 'DELIVERED' }))

    renderDetail()

    await waitFor(() => expect(screen.getByText('转为「待验收」')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('实际人天'), { target: { value: '0' } })

    fireEvent.click(screen.getByText('转为「待验收」'))

    await waitFor(() =>
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/legacy/bounties/${BOUNTY_ID}/transition`, {
        to: 'DELIVERED',
        retrospective: undefined,
        person_days: 0,
      }),
    )
  })

  it('sends the chosen completion level when accepting a delivery into COMPLETED', async () => {
    mockGetsFor(makeBounty({ status: 'DELIVERED', sponsor_id: SPONSOR }))
    mockIdentity(SPONSOR_USER)
    mockedApiPost.mockResolvedValue(makeBounty({ status: 'COMPLETED' }))

    renderDetail()

    await waitFor(() => expect(screen.getByText('转为「已完成」')).toBeInTheDocument())

    // Default completion is MET; pick a different one so the assertion below
    // actually proves the chosen value round-trips, not just some default.
    fireEvent.change(screen.getByLabelText('完成度档'), { target: { value: 'EXCEEDED' } })
    fireEvent.click(screen.getByText('转为「已完成」'))

    await waitFor(() =>
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/legacy/bounties/${BOUNTY_ID}/transition`, {
        to: 'COMPLETED',
        retrospective: undefined,
        person_days: undefined,
        completion: 'EXCEEDED',
      }),
    )
  })

  it('does not offer the difficulty control to a sponsor, but does offer it to a tech lead', async () => {
    mockGetsFor(makeBounty({ status: 'OPEN', sponsor_id: SPONSOR }))
    mockIdentity(SPONSOR_USER)

    renderDetail()

    await waitFor(() => expect(screen.getByText('竞价链路降延迟')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: '设置难度档' })).not.toBeInTheDocument()

    cleanup()
    mockGetsFor(makeBounty({ status: 'OPEN', sponsor_id: SPONSOR }))
    mockIdentity(TECH_LEAD_USER)

    renderDetail()

    await waitFor(() => expect(screen.getByRole('button', { name: '设置难度档' })).toBeInTheDocument())
  })

  it('shows the value-level control while the bounty is OPEN, and hides it once claimed', async () => {
    mockGetsFor(makeBounty({ status: 'OPEN', sponsor_id: SPONSOR }))
    mockIdentity(SPONSOR_USER)

    renderDetail()

    await waitFor(() => expect(screen.getByRole('button', { name: '设置价值档' })).toBeInTheDocument())

    cleanup()
    mockGetsFor(makeBounty({ status: 'CLAIMED', sponsor_id: SPONSOR, claimed_by: ENGINEER }))
    mockIdentity(SPONSOR_USER)

    renderDetail()

    await waitFor(() => expect(screen.getByText('已认领', { exact: true })).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: '设置价值档' })).not.toBeInTheDocument()
  })

  it('offers the steward correction channel on a terminal work to a steward, and hides it from a non-steward', async () => {
    mockGetsFor(makeBounty({ status: 'COMPLETED', sponsor_id: SPONSOR }))
    mockIdentity(STEWARD_USER)

    renderDetail()

    await waitFor(() => expect(screen.getByText('Steward 修正通道')).toBeInTheDocument())

    cleanup()
    mockGetsFor(makeBounty({ status: 'COMPLETED', sponsor_id: SPONSOR }))
    mockIdentity(ENGINEER_USER)

    renderDetail()

    await waitFor(() => expect(screen.getByText('已归档为作品,不可再流转。')).toBeInTheDocument())
    expect(screen.queryByText('Steward 修正通道')).not.toBeInTheDocument()
  })

  it('shows the settled score on the detail page once the bounty has one (the one place it may appear)', async () => {
    mockGetsFor(makeBounty({ status: 'COMPLETED', settled_score: 17.5, settled_at: '2026-04-01T00:00:00Z' }))

    renderDetail()

    await waitFor(() => expect(screen.getByText(/结算分值:17.5/)).toBeInTheDocument())
  })
})
