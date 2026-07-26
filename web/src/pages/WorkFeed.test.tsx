import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import WorkFeed from './WorkFeed'
import { apiGet } from '../api/client'
import type { Bounty, WorkView } from '../types'

// Mocks the whole module so apiGet's resolution/rejection is fully
// controllable per test, without touching global fetch or a real backend.
vi.mock('../api/client')

const mockedApiGet = vi.mocked(apiGet)

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup (which hooks a global afterEach)
// never registers; without this, a component rendered by one test stays
// mounted in the DOM and pollutes the next test's queries. Mirrors the
// pattern already established in src/identity.test.tsx and
// src/components/WorkCard.test.tsx.
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const PM = '00000000-0000-0000-0000-000000000001'

function makeBounty(overrides: Partial<Bounty> = {}): Bounty {
  return {
    id: '00000000-0000-0000-0000-0000000000b1',
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

function renderFeed() {
  return render(
    <MemoryRouter>
      <WorkFeed />
    </MemoryRouter>,
  )
}

describe('WorkFeed', () => {
  it('shows the loading hint while the request is pending, and not the empty-state text (B1, regression guard for A)', async () => {
    let resolveWorks!: (works: WorkView[]) => void
    mockedApiGet.mockReturnValue(
      new Promise<WorkView[]>((resolve) => {
        resolveWorks = resolve
      }),
    )

    renderFeed()

    expect(screen.getByText('正在加载作品流…')).toBeInTheDocument()
    expect(screen.queryByText('还没有已完成的作品。')).not.toBeInTheDocument()

    // Resolve so the pending promise doesn't leak into later tests.
    resolveWorks([])
    await waitFor(() => expect(screen.queryByText('正在加载作品流…')).not.toBeInTheDocument())
  })

  it('shows the empty-state text once the request resolves with no works (B2)', async () => {
    mockedApiGet.mockResolvedValue([])

    renderFeed()

    await waitFor(() => expect(screen.getByText('还没有已完成的作品。')).toBeInTheDocument())
    expect(screen.queryByText('正在加载作品流…')).not.toBeInTheDocument()
  })

  it('renders every work title once the request resolves with data (B3)', async () => {
    const works: WorkView[] = [
      { bounty: makeBounty({ id: 'b-1', title: '竞价链路降延迟' }), credits: [] },
      { bounty: makeBounty({ id: 'b-2', title: '出价策略重构' }), credits: [] },
    ]
    mockedApiGet.mockResolvedValue(works)

    renderFeed()

    await waitFor(() => expect(screen.getByText('竞价链路降延迟')).toBeInTheDocument())
    expect(screen.getByText('出价策略重构')).toBeInTheDocument()
    expect(screen.queryByText('正在加载作品流…')).not.toBeInTheDocument()
    expect(screen.queryByText('还没有已完成的作品。')).not.toBeInTheDocument()
  })

  it('renders the failure message including the underlying error message when the request rejects (B4)', async () => {
    mockedApiGet.mockRejectedValue(new Error('network down'))

    renderFeed()

    await waitFor(() => expect(document.querySelector('.error')).not.toBeNull())
    const text = document.querySelector('.error')?.textContent ?? ''
    expect(text).toContain('network down')
    expect(text).not.toContain('[object Object]')
    expect(text).not.toBe('加载失败:')
  })
})
