import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { MemoryRouter } from 'react-router-dom'
import Board from './Board'
import { apiGet } from '../api/client'
import { useIdentity } from '../identity'
import type { Bounty, User } from '../types'

// Mocks both modules so apiGet resolution and the current identity are
// fully controllable per test, without touching global fetch, a real
// backend, or IdentityProvider. Mirrors the pattern established in
// src/pages/Mine.test.tsx and src/pages/Portfolio.test.tsx.
vi.mock('../api/client')
vi.mock('../identity')

const mockedApiGet = vi.mocked(apiGet)
const mockedUseIdentity = vi.mocked(useIdentity)

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this, a
// component rendered by one test stays mounted and pollutes the next test's
// queries. Mirrors src/pages/Mine.test.tsx.
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const SPONSOR_A = '00000000-0000-0000-0000-000000000001'
const ENGINEER_D = '00000000-0000-0000-0000-000000000004'

const SPONSOR_USER: User = { id: SPONSOR_A, name: '产品 A', email: 'a@example.com', roles: ['SPONSOR'], active: true }
const ENGINEER_USER: User = { id: ENGINEER_D, name: '研发 D', email: 'd@example.com', roles: ['ENGINEER'], active: true }
const USERS: User[] = [SPONSOR_USER, ENGINEER_USER]

function makeBounty(overrides: Partial<Bounty> = {}): Bounty {
  return {
    id: 'draft-1',
    type: 'DELIVERY',
    title: 'A 的私有草稿',
    goal: '',
    acceptance_criteria: '',
    visibility: 'PUBLIC',
    business_lines: [],
    commitment: 'COMMITTED',
    status: 'DRAFT',
    sponsor_id: SPONSOR_A,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// A thin harness that reassigns the mocked useIdentity() return value on
// every render, so a click can flip the "current identity" the mounted
// <Board/> observes — the same thing the real header's UserSwitcher does by
// calling switchTo(), without unmounting the page. Board itself calls
// useIdentity() on every render, so reassigning the mock here (which runs
// before Board's own render, since this component is Board's parent) is
// observed by Board on the very next render.
function IdentitySwitchHarness() {
  const [meId, setMeId] = useState(SPONSOR_A)
  const me = USERS.find((u) => u.id === meId) ?? null
  mockedUseIdentity.mockReturnValue({ me, users: USERS, switchTo: setMeId })
  return (
    <>
      <button onClick={() => setMeId(ENGINEER_D)}>switch-to-engineer-d</button>
      <Board />
    </>
  )
}

function renderBoard() {
  return render(
    <MemoryRouter>
      <IdentitySwitchHarness />
    </MemoryRouter>,
  )
}

describe('Board — identity switch refetch', () => {
  it("switching identity refetches an already-mounted board and replaces the previous identity's data (repro: sponsor's draft must not stay on screen after switching to an engineer with no permission to see it)", async () => {
    mockedApiGet.mockResolvedValueOnce([makeBounty()])

    renderBoard()

    await waitFor(() => expect(screen.getByText('A 的私有草稿')).toBeInTheDocument())
    expect(mockedApiGet).toHaveBeenCalledTimes(1)

    // The engineer has no permission to see the draft; the server would
    // return an empty list for them.
    mockedApiGet.mockResolvedValueOnce([])

    fireEvent.click(screen.getByText('switch-to-engineer-d'))

    // The new identity's (empty) data replaces the old: the draft is gone
    // and the empty-state text appears, on the same mounted page, with no
    // navigation.
    await waitFor(() => expect(screen.queryByText('A 的私有草稿')).not.toBeInTheDocument())
    await waitFor(() => expect(screen.getByText('没有符合条件的单。')).toBeInTheDocument())
    expect(mockedApiGet).toHaveBeenCalledTimes(2)
  })

  it("does not show the previous identity's rows while the new identity's request is still in flight", async () => {
    mockedApiGet.mockResolvedValueOnce([makeBounty()])

    renderBoard()

    await waitFor(() => expect(screen.getByText('A 的私有草稿')).toBeInTheDocument())

    let resolveEngineerFetch!: (bounties: Bounty[]) => void
    mockedApiGet.mockReturnValueOnce(
      new Promise<Bounty[]>((resolve) => {
        resolveEngineerFetch = resolve
      }),
    )

    fireEvent.click(screen.getByText('switch-to-engineer-d'))

    // Before the new request resolves, the sponsor's draft must already be
    // gone — replaced by the loading state, not left on screen.
    await waitFor(() => expect(screen.getByText('正在加载看板…')).toBeInTheDocument())
    expect(screen.queryByText('A 的私有草稿')).not.toBeInTheDocument()

    resolveEngineerFetch([])
    await waitFor(() => expect(screen.getByText('没有符合条件的单。')).toBeInTheDocument())
  })
})
