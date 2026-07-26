import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import BountyDetail from './BountyDetail'
import { apiGet, apiPost } from '../api/client'
import type { Bounty } from '../types'

// Mocks the whole module so apiGet/apiPost resolution/rejection is fully
// controllable per test, without touching global fetch, a real backend, or
// IdentityProvider. Mirrors the pattern established in
// src/pages/WorkFeed.test.tsx and src/components/CreditPanel.test.tsx.
vi.mock('../api/client')

const mockedApiGet = vi.mocked(apiGet)
const mockedApiPost = vi.mocked(apiPost)

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

// BountyDetail renders CreditPanel, which issues its own apiGet call for
// `/api/bounties/:id/credits`. Route both calls through one mock so the page
// can render fully without a real backend or IdentityProvider (CreditPanel's
// useIdentity() falls back to its context default — me: null — which is fine
// here since none of these tests exercise nomination).
function mockGetsFor(bounty: Bounty) {
  mockedApiGet.mockImplementation((path: unknown) => {
    if (typeof path === 'string' && path.endsWith('/credits')) {
      return Promise.resolve([])
    }
    return Promise.resolve(bounty)
  })
}

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={[`/bounties/${BOUNTY_ID}`]}>
      <Routes>
        <Route path="/bounties/:id" element={<BountyDetail />} />
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
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/bounties/${BOUNTY_ID}/transition`, {
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
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/bounties/${BOUNTY_ID}/transition`, {
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
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/bounties/${BOUNTY_ID}/transition`, {
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
      expect(mockedApiPost).toHaveBeenCalledWith(`/api/bounties/${BOUNTY_ID}/transition`, {
        to: 'DELIVERED',
        retrospective: undefined,
        person_days: 0,
      }),
    )
  })
})
