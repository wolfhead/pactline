import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import App from './App'
import { useIdentity } from './identity'

vi.mock('./identity', () => ({ useIdentity: vi.fn() }))
vi.mock('./pages/tasks/TaskPage', () => ({
  default: () => <h1>Protected standalone Task page</h1>,
}))
vi.mock('./components/tasks/NavSidebar', () => ({ default: () => <nav>Navigation</nav> }))
vi.mock('./components/tasks/TaskComposer', () => ({
  TaskComposerProvider: ({ children }: { children: React.ReactNode }) => children,
  useTaskComposer: () => ({ openTaskComposer: vi.fn() }),
}))

const approved = {
  id: 'u1', name: 'Admin', email: 'admin@example.test', avatar_url: null,
  platform_role: 'ADMIN' as const, access_status: 'APPROVED' as const,
  roles: [], active: true, created_at: '', updated_at: '',
}

function setIdentity(overrides: Record<string, unknown>) {
  vi.mocked(useIdentity).mockReturnValue({
    status: 'authenticated',
    actor: approved,
    subject: approved,
    me: approved,
    users: [approved],
    impersonation: null,
    isReadOnly: false,
    error: null,
    refresh: vi.fn(),
    loginForDevelopment: vi.fn(),
    logout: vi.fn(),
    endImpersonation: vi.fn(),
    ...overrides,
  })
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('standalone Task route identity boundaries', () => {
  it('waits for identity bootstrap before rendering the Task page', () => {
    setIdentity({ status: 'loading', actor: null, subject: null, me: null, users: [] })
    render(<MemoryRouter initialEntries={['/tasks/142']}><App /></MemoryRouter>)

    expect(screen.getByText('正在确认登录状态…')).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Protected standalone Task page' }))
      .not.toBeInTheDocument()
  })

  it('redirects an unauthenticated deep link to login', async () => {
    setIdentity({ status: 'unauthenticated', actor: null, subject: null, me: null, users: [] })
    render(<MemoryRouter initialEntries={['/tasks/142']}><App /></MemoryRouter>)

    expect(await screen.findByRole('heading', { name: '登录 Pactline' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Protected standalone Task page' }))
      .not.toBeInTheDocument()
  })

  it('keeps a pending member on the access decision page', () => {
    const pending = { ...approved, platform_role: 'MEMBER' as const, access_status: 'PENDING' as const }
    setIdentity({ actor: pending, subject: pending, me: pending, users: [pending] })
    render(<MemoryRouter initialEntries={['/tasks/142']}><App /></MemoryRouter>)

    expect(screen.getByRole('heading', { name: '访问申请等待审批' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Protected standalone Task page' }))
      .not.toBeInTheDocument()
  })

  it('keeps the Task route inside impersonation read-only protection', () => {
    const member = { ...approved, id: 'u2', name: 'Member', platform_role: 'MEMBER' as const }
    setIdentity({
      subject: member,
      me: member,
      impersonation: {
        id: 'imp-1', session_id: 'session-1', actor_user_id: approved.id,
        subject_user_id: member.id, started_at: '',
      },
      isReadOnly: true,
    })
    render(<MemoryRouter initialEntries={['/tasks/142']}><App /></MemoryRouter>)

    expect(screen.getByRole('heading', { name: 'Protected standalone Task page' })).toBeVisible()
    expect(screen.getByText('管理员 Admin 正以 Member 身份只读查看')).toBeVisible()
    expect(screen.getByRole('main')).toHaveAttribute('data-read-only', 'true')
  })
})
