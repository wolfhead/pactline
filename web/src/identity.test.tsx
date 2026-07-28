import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import { IdentityProvider, useIdentity, type User } from './identity'
import { getCurrentUserId } from './api/client'

const STORAGE_KEY = 'bountyboard.currentUserId'
const SEED_PM = '00000000-0000-0000-0000-000000000001'
const OTHER_USER = '00000000-0000-0000-0000-000000000002'

const USERS: User[] = [
  {
    id: SEED_PM,
    name: 'Alice',
    email: 'alice@example.com',
    avatar_url: null,
    platform_role: 'MEMBER',
    roles: ['SPONSOR'],
    active: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  {
    id: OTHER_USER,
    name: 'Bob',
    email: 'bob@example.com',
    avatar_url: null,
    platform_role: 'MEMBER',
    roles: ['ENGINEER'],
    active: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
]

let observedIdAfterSwitch = ''

/** Calls switchTo and records getCurrentUserId() synchronously, still
 * inside the event handler, before React has a chance to commit the state
 * update or run any effect. */
function SwitchProbe() {
  const { switchTo } = useIdentity()
  return (
    <button
      onClick={() => {
        switchTo(OTHER_USER)
        observedIdAfterSwitch = getCurrentUserId()
      }}
    >
      switch
    </button>
  )
}

describe('IdentityProvider', () => {
  beforeEach(() => {
    localStorage.clear()
    observedIdAfterSwitch = ''
    vi.restoreAllMocks()
  })

  // vitest.config's test block doesn't set `globals: true`, so
  // @testing-library/react's own auto-cleanup (which hooks a global
  // afterEach) never registers; without this, a component rendered by one
  // test stays mounted in the DOM and pollutes the next test's queries.
  afterEach(() => {
    cleanup()
  })

  it('updates getCurrentUserId synchronously in switchTo, before any effect runs (A3)', async () => {
    // Each call gets its own Response instance: switchTo triggers a second
    // /api/users fetch (for the new identity) once the effect re-runs, and a
    // Response body can only be read once.
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() =>
        Promise.resolve(new Response(JSON.stringify(USERS), { status: 200 })),
      ),
    )

    render(
      <IdentityProvider>
        <SwitchProbe />
      </IdentityProvider>,
    )

    await waitFor(() => screen.getByText('switch'))
    expect(getCurrentUserId()).toBe(SEED_PM)

    fireEvent.click(screen.getByText('switch'))

    // Read by the handler itself, in the same synchronous call as switchTo:
    // this is strictly before React commits the resulting state update, and
    // therefore strictly before IdentityProvider's [meId] effect could have
    // run. If the id were assigned only inside that effect, this would
    // still read the previous (seed) id here.
    expect(observedIdAfterSwitch).toBe(OTHER_USER)
  })

  it('recovers from a stale localStorage id that 401s: falls back, retries, clears the bad value (A1)', async () => {
    localStorage.setItem(STORAGE_KEY, 'stale-user-id')

    let calls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => {
        calls += 1
        if (calls === 1) {
          return Promise.resolve(
            new Response(JSON.stringify({ error: 'unknown user' }), { status: 401 }),
          )
        }
        return Promise.resolve(new Response(JSON.stringify(USERS), { status: 200 }))
      }),
    )

    render(
      <IdentityProvider>
        <div data-testid="children">loaded</div>
      </IdentityProvider>,
    )

    expect(screen.getByText('正在加载用户…')).toBeInTheDocument()

    await waitFor(() => expect(screen.getByTestId('children')).toBeInTheDocument())

    expect(calls).toBe(2)
    expect(localStorage.getItem(STORAGE_KEY)).not.toBe('stale-user-id')
    expect(localStorage.getItem(STORAGE_KEY)).toBe(SEED_PM)
  })

  it('shows a visible Chinese error state when /api/users keeps failing, instead of loading forever (A1)', async () => {
    localStorage.setItem(STORAGE_KEY, 'stale-user-id')

    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() =>
        Promise.resolve(new Response(JSON.stringify({ error: 'unknown user' }), { status: 401 })),
      ),
    )

    render(
      <IdentityProvider>
        <div data-testid="children">loaded</div>
      </IdentityProvider>,
    )

    await waitFor(() => {
      expect(screen.queryByText('正在加载用户…')).not.toBeInTheDocument()
    })

    expect(screen.queryByTestId('children')).not.toBeInTheDocument()
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent(/加载用户列表失败/)
  })
})
