import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import Portfolio from './Portfolio'
import { apiGet } from '../api/client'
import { useIdentity } from '../identity'
import type { Bounty, User, WorkView } from '../types'

// Mocks both modules so apiGet resolution and the current identity are fully
// controllable per test, without touching global fetch, a real backend, or
// IdentityProvider. Mirrors the pattern established in
// src/pages/WorkFeed.test.tsx and src/components/CreditPanel.test.tsx.
vi.mock('../api/client')
vi.mock('../identity')

const mockedApiGet = vi.mocked(apiGet)
const mockedUseIdentity = vi.mocked(useIdentity)

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this, a
// component rendered by one test stays mounted and pollutes the next test's
// queries. Mirrors src/pages/WorkFeed.test.tsx.
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const NOMINEE = '00000000-0000-0000-0000-000000000003'
const SPONSOR = '00000000-0000-0000-0000-000000000001'

const USERS: User[] = [{ id: NOMINEE, name: '研发 D', email: 'd@example.com', roles: ['ENGINEER'], active: true }]

function makeBounty(overrides: Partial<Bounty> = {}): Bounty {
  return {
    id: '00000000-0000-0000-0000-0000000000b1',
    type: 'DELIVERY',
    title: '竞价链路降延迟',
    goal: '',
    acceptance_criteria: '',
    visibility: 'PUBLIC',
    business_lines: [],
    commitment: 'COMMITTED',
    status: 'COMPLETED',
    sponsor_id: SPONSOR,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function mockIdentity() {
  mockedUseIdentity.mockReturnValue({ me: null, users: USERS, switchTo: vi.fn() })
}

function renderPortfolio() {
  return render(
    <MemoryRouter initialEntries={[`/users/${NOMINEE}/portfolio`]}>
      <Routes>
        <Route path="/users/:id/portfolio" element={<Portfolio />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('Portfolio', () => {
  it('groups works by credit role, and shows a work under both groups when the person holds two roles on it (product rule: group by role, no ranking)', async () => {
    mockIdentity()
    // One work where NOMINEE holds two different roles (REVIEW and
    // SUPPORT) — it must appear in both role groups, not just one.
    const workA: WorkView = {
      bounty: makeBounty({ id: 'b-a', title: '竞价链路降延迟' }),
      credits: [
        {
          credit: { id: 'c1', bounty_id: 'b-a', user_id: NOMINEE, role: 'REVIEW', status: 'CONFIRMED', created_at: '2026-01-01T00:00:00Z' },
          user_name: '研发 D',
        },
        {
          credit: { id: 'c2', bounty_id: 'b-a', user_id: NOMINEE, role: 'SUPPORT', status: 'CONFIRMED', created_at: '2026-01-01T00:00:00Z' },
          user_name: '研发 D',
        },
      ],
    }
    const workB: WorkView = {
      bounty: makeBounty({ id: 'b-b', title: '出价策略重构' }),
      credits: [
        {
          credit: { id: 'c3', bounty_id: 'b-b', user_id: NOMINEE, role: 'REVIEW', status: 'CONFIRMED', created_at: '2026-01-01T00:00:00Z' },
          user_name: '研发 D',
        },
      ],
    }
    mockedApiGet.mockResolvedValue([workA, workB])

    renderPortfolio()

    await waitFor(() => expect(screen.getByText('深度评审（2）')).toBeInTheDocument())
    expect(screen.getByText('上下文支援（1）')).toBeInTheDocument()

    const reviewGroup = screen.getByText('深度评审（2）').closest('div')!
    expect(within(reviewGroup).getByText('竞价链路降延迟')).toBeInTheDocument()
    expect(within(reviewGroup).getByText('出价策略重构')).toBeInTheDocument()

    const supportGroup = screen.getByText('上下文支援（1）').closest('div')!
    expect(within(supportGroup).getByText('竞价链路降延迟')).toBeInTheDocument()
    expect(within(supportGroup).queryByText('出价策略重构')).not.toBeInTheDocument()

    // No ranking, no totals: this is a role breakdown, not a score card.
    expect(screen.queryByText(/排名|总分|score/i)).not.toBeInTheDocument()
  })

  it('shows the empty-state text only after loading resolves, never while the request is pending (regression guard, cf. WorkFeed/Board)', async () => {
    mockIdentity()
    let resolveWorks!: (works: WorkView[]) => void
    mockedApiGet.mockReturnValue(
      new Promise<WorkView[]>((resolve) => {
        resolveWorks = resolve
      }),
    )

    renderPortfolio()

    expect(screen.getByText('加载中…')).toBeInTheDocument()
    expect(screen.queryByText('还没有已确认署名的作品。')).not.toBeInTheDocument()

    resolveWorks([])

    await waitFor(() => expect(screen.getByText('还没有已确认署名的作品。')).toBeInTheDocument())
    expect(screen.queryByText('加载中…')).not.toBeInTheDocument()
  })
})
