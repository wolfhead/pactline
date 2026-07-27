import { describe, expect, it, afterEach } from 'vitest'
import { render, screen, cleanup, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import WorkCard from './WorkCard'
import type { Bounty, Credit, CreditView, WorkView } from '../../types'

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup (which hooks a global afterEach)
// never registers; without this, a component rendered by one test stays
// mounted in the DOM and pollutes the next test's queries. Mirrors the
// pattern already established in src/identity.test.tsx.
afterEach(() => {
  cleanup()
})

const BOUNTY_ID = '00000000-0000-0000-0000-0000000000b1'
const PM = '00000000-0000-0000-0000-000000000001'
const ENGINEER_A = '00000000-0000-0000-0000-000000000002'
const ENGINEER_B = '00000000-0000-0000-0000-000000000003'

function makeBounty(overrides: Partial<Bounty> = {}): Bounty {
  return {
    id: BOUNTY_ID,
    type: 'DELIVERY',
    title: '竞价链路降延迟',
    goal: 'P99 80ms → 45ms',
    acceptance_criteria: '',
    visibility: 'PUBLIC',
    business_lines: [{ tag: 'DSP', weight: 1 }],
    commitment: 'COMMITTED',
    status: 'COMPLETED',
    sponsor_id: PM,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function makeCredit(overrides: Partial<Credit> = {}): Credit {
  return {
    id: crypto.randomUUID(),
    bounty_id: BOUNTY_ID,
    user_id: ENGINEER_A,
    role: 'LEAD',
    status: 'CONFIRMED',
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function renderCard(work: WorkView) {
  return render(
    <MemoryRouter>
      <WorkCard work={work} />
    </MemoryRouter>,
  )
}

describe('WorkCard', () => {
  it('renders the retrospective text for ABANDONED work', () => {
    const work: WorkView = {
      bounty: makeBounty({
        status: 'ABANDONED',
        retrospective: '第三方依赖不稳定，风险超出预期，暂停投入。',
      }),
      credits: [],
    }

    renderCard(work)

    expect(screen.getByText(/第三方依赖不稳定，风险超出预期，暂停投入。/)).toBeInTheDocument()
  })

  it('does not render a retrospective block for COMPLETED work', () => {
    const work: WorkView = {
      bounty: makeBounty({
        status: 'COMPLETED',
        // Even if a retrospective field happens to be populated, it must
        // not be surfaced unless the bounty is ABANDONED.
        retrospective: '不应该被渲染',
      }),
      credits: [],
    }

    const { container } = renderCard(work)

    expect(screen.queryByText(/不应该被渲染/)).not.toBeInTheDocument()
    expect(container.querySelector('.retro')).toBeNull()
  })

  it('renders every credit with its Chinese role label, in the given order, without reordering', () => {
    const credits: CreditView[] = [
      { credit: makeCredit({ id: 'c-support', user_id: ENGINEER_B, role: 'SUPPORT' }), user_name: 'Bob' },
      { credit: makeCredit({ id: 'c-lead', user_id: ENGINEER_A, role: 'LEAD' }), user_name: 'Alice' },
      { credit: makeCredit({ id: 'c-review', user_id: PM, role: 'REVIEW' }), user_name: 'Carol' },
    ]
    const work: WorkView = {
      bounty: makeBounty(),
      credits,
    }

    renderCard(work)

    const list = screen.getByRole('list')
    const items = within(list).getAllByRole('listitem')

    expect(items).toHaveLength(3)
    // Order must match the input order exactly (SUPPORT, LEAD, REVIEW) —
    // the frontend must not re-sort credits the backend already sorted.
    expect(within(items[0]).getByText('上下文支援')).toBeInTheDocument()
    expect(within(items[0]).getByText('Bob')).toBeInTheDocument()
    expect(within(items[1]).getByText('主交付')).toBeInTheDocument()
    expect(within(items[1]).getByText('Alice')).toBeInTheDocument()
    expect(within(items[2]).getByText('深度评审')).toBeInTheDocument()
    expect(within(items[2]).getByText('Carol')).toBeInTheDocument()
  })

  it('never renders a settled score, even if one is present on the bounty (a score is a fact about a work, never a person; the feed and every portfolio are exactly where it must not leak)', () => {
    const work: WorkView = {
      // The real API (decorate() in internal/legacy/api/feed_handler.go) never
      // sends settled_score/settled_at inside a WorkView at all — but this
      // guards WorkCard itself, independent of what the backend currently
      // does, against ever being the component that renders one.
      bounty: makeBounty({ settled_score: 42, settled_at: '2026-04-01T00:00:00Z' }),
      credits: [],
    }

    renderCard(work)

    expect(screen.queryByText(/42/)).not.toBeInTheDocument()
    expect(screen.queryByText(/排名|排行榜|总分|得分|积分|score/i)).not.toBeInTheDocument()
  })
})
