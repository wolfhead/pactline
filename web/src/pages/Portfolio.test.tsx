import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
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
const OTHER = '00000000-0000-0000-0000-000000000004'

const USERS: User[] = [
  { id: NOMINEE, name: '研发 D', email: 'd@example.com', roles: ['ENGINEER'], active: true },
  { id: OTHER, name: '研发 E', email: 'e@example.com', roles: ['ENGINEER'], active: true },
]

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

// Navigates within the same MemoryRouter to another user's portfolio, on
// click. React Router keeps the Portfolio component instance mounted across
// this navigation (only the :id param changes for the same matched route),
// so the component's own effect — not a remount — is what has to guard
// against the stale-response race (C2).
function NavigateButton({ to }: { to: string }) {
  const navigate = useNavigate()
  return <button onClick={() => navigate(to)}>go-to-other</button>
}

function renderPortfolioWithNav() {
  return render(
    <MemoryRouter initialEntries={[`/users/${NOMINEE}/portfolio`]}>
      <NavigateButton to={`/users/${OTHER}/portfolio`} />
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

  it("does not show another person's role credit under the viewed user's portfolio (product rule: this person's work, not any collaborator's) (A1)", async () => {
    mockIdentity()
    // OTHER holds REVIEW on this work; NOMINEE (the viewed user) holds no
    // credit on it at all. If the filter dropped its `user_id === id`
    // clause, matching would fall back to role-only, and this work would
    // wrongly surface under NOMINEE's 深度评审 group.
    const workOther: WorkView = {
      bounty: makeBounty({ id: 'b-other', title: '别人的评审工作' }),
      credits: [
        {
          credit: { id: 'c-other', bounty_id: 'b-other', user_id: OTHER, role: 'REVIEW', status: 'CONFIRMED', created_at: '2026-01-01T00:00:00Z' },
          user_name: '研发 E',
        },
      ],
    }
    mockedApiGet.mockResolvedValue([workOther])

    renderPortfolio()

    await waitFor(() => expect(screen.queryByText('加载中…')).not.toBeInTheDocument())

    // No role group can legitimately match for NOMINEE, so no role header
    // renders, and the other person's work title never appears.
    expect(screen.queryByText(/深度评审/)).not.toBeInTheDocument()
    expect(screen.queryByText('别人的评审工作')).not.toBeInTheDocument()
  })

  it("shows a work under exactly the viewed user's own role group when other people hold different roles on the same work (A2)", async () => {
    mockIdentity()
    // NOMINEE holds DEFINE; OTHER holds LEAD on the very same work. Only
    // the DEFINE group should pick it up for NOMINEE's portfolio.
    const workMix: WorkView = {
      bounty: makeBounty({ id: 'b-mix', title: '混合署名的工作' }),
      credits: [
        {
          credit: { id: 'c-mine', bounty_id: 'b-mix', user_id: NOMINEE, role: 'DEFINE', status: 'CONFIRMED', created_at: '2026-01-01T00:00:00Z' },
          user_name: '研发 D',
        },
        {
          credit: { id: 'c-theirs', bounty_id: 'b-mix', user_id: OTHER, role: 'LEAD', status: 'CONFIRMED', created_at: '2026-01-01T00:00:00Z' },
          user_name: '研发 E',
        },
      ],
    }
    mockedApiGet.mockResolvedValue([workMix])

    renderPortfolio()

    await waitFor(() => expect(screen.getByText('定义方案（1）')).toBeInTheDocument())
    const defineGroup = screen.getByText('定义方案（1）').closest('div')!
    expect(within(defineGroup).getByText('混合署名的工作')).toBeInTheDocument()

    // OTHER's LEAD role must not surface as a group heading for NOMINEE.
    // WorkCard legitimately lists every collaborator's role (including
    // OTHER's LEAD) on the card itself, so assert on the group heading
    // text (which carries a count) rather than the bare role label.
    expect(screen.queryByText('主交付（1）')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 3, name: /^主交付/ })).not.toBeInTheDocument()
  })

  it("does not let a stale response for a previous route id overwrite the current id's works (C2: request cancellation on id change)", async () => {
    mockIdentity()

    let resolveNominee!: (works: WorkView[]) => void
    let resolveOther!: (works: WorkView[]) => void

    // Two independent, individually controllable requests: one for the
    // initial route (NOMINEE), one for the route navigated to (OTHER).
    mockedApiGet.mockImplementation((path: unknown) => {
      if (typeof path === 'string' && path === `/api/users/${NOMINEE}/portfolio`) {
        return new Promise<WorkView[]>((resolve) => {
          resolveNominee = resolve
        })
      }
      if (typeof path === 'string' && path === `/api/users/${OTHER}/portfolio`) {
        return new Promise<WorkView[]>((resolve) => {
          resolveOther = resolve
        })
      }
      return Promise.resolve([])
    })

    renderPortfolioWithNav()

    // The initial render fires the request for NOMINEE.
    await waitFor(() => expect(resolveNominee).toBeDefined())

    // Navigate to OTHER's portfolio before NOMINEE's request resolves. React
    // Router updates the :id param without unmounting Portfolio, so this
    // exercises the component's own id-change effect, not a fresh mount.
    fireEvent.click(screen.getByText('go-to-other'))

    await waitFor(() => expect(resolveOther).toBeDefined())

    const othersWork: WorkView = {
      bounty: makeBounty({ id: 'b-other', title: 'OTHER的作品' }),
      credits: [
        {
          credit: {
            id: 'c-other',
            bounty_id: 'b-other',
            user_id: OTHER,
            role: 'DEFINE',
            status: 'CONFIRMED',
            created_at: '2026-01-01T00:00:00Z',
          },
          user_name: '研发 E',
        },
      ],
    }

    // The later (OTHER) request — for the page as currently displayed —
    // resolves first.
    resolveOther([othersWork])
    await waitFor(() => expect(screen.getByText('OTHER的作品')).toBeInTheDocument())

    // The earlier (NOMINEE) request — for a route already navigated away
    // from — resolves late, with an empty result. Without the cancellation
    // guard this overwrites `works`, wiping OTHER's already-rendered work
    // off the page OTHER's own heading is displaying (the wrong person's
    // — here, empty — data rendered under the current person's heading).
    resolveNominee([])

    // Give the late .then a tick to run (and, if unguarded, overwrite
    // state) before asserting nothing changed.
    await waitFor(() => expect(screen.getByText('OTHER的作品')).toBeInTheDocument())
    expect(screen.queryByText('还没有已确认署名的作品。')).not.toBeInTheDocument()
  })
})
