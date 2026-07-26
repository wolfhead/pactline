import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import NewBountyForm from './NewBountyForm'
import { apiPost } from '../api/client'
import type { Bounty } from '../types'

// Mocks the whole module so apiPost's resolution/rejection is fully
// controllable per test, without touching global fetch or a real backend.
// Mirrors the pattern established in src/pages/WorkFeed.test.tsx.
vi.mock('../api/client')

const mockedApiPost = vi.mocked(apiPost)

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this, a
// component rendered by one test stays mounted and pollutes the next test's
// queries. Mirrors src/pages/WorkFeed.test.tsx and src/components/WorkCard.test.tsx.
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const TITLE_PLACEHOLDER = '标题'
const TAGS_PLACEHOLDER = '业务线,如 DSP:0.7,ADX:0.3 或 PLATFORM:1'
const RESTRICTION_PLACEHOLDER = '限定条件——写上下文,不写职级(例:需要 Bidding Engine 上下文)'

function makeBounty(overrides: Partial<Bounty> = {}): Bounty {
  return {
    id: 'b-1',
    type: 'DELIVERY',
    title: '新单',
    goal: '',
    acceptance_criteria: '',
    visibility: 'PUBLIC',
    business_lines: [],
    commitment: 'COMMITTED',
    status: 'DRAFT',
    sponsor_id: 'u-1',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function submitForm() {
  fireEvent.click(screen.getByRole('button', { name: /创建草稿|提交中…/ }))
}

describe('NewBountyForm', () => {
  it('parses "DSP:0.7,ADX:0.3" into weighted business lines on submit', async () => {
    mockedApiPost.mockResolvedValue(makeBounty())
    const onCreated = vi.fn()

    render(<NewBountyForm onCreated={onCreated} />)

    fireEvent.change(screen.getByPlaceholderText(TITLE_PLACEHOLDER), { target: { value: '竞价链路降延迟' } })
    fireEvent.change(screen.getByPlaceholderText(TAGS_PLACEHOLDER), { target: { value: 'DSP:0.7,ADX:0.3' } })

    submitForm()

    await waitFor(() => expect(mockedApiPost).toHaveBeenCalled())
    const [path, body] = mockedApiPost.mock.calls[0]
    expect(path).toBe('/api/bounties')
    expect((body as { business_lines: unknown }).business_lines).toEqual([
      { tag: 'DSP', weight: 0.7 },
      { tag: 'ADX', weight: 0.3 },
    ])
  })

  it('reveals the restriction input only when the limited pool is chosen', () => {
    render(<NewBountyForm onCreated={vi.fn()} />)

    // Default visibility is the public pool: no restriction input.
    expect(screen.queryByPlaceholderText(RESTRICTION_PLACEHOLDER)).not.toBeInTheDocument()

    fireEvent.change(screen.getByDisplayValue('公开池'), { target: { value: 'RESTRICTED' } })
    expect(screen.getByPlaceholderText(RESTRICTION_PLACEHOLDER)).toBeInTheDocument()

    fireEvent.change(screen.getByDisplayValue('限定池'), { target: { value: 'PUBLIC' } })
    expect(screen.queryByPlaceholderText(RESTRICTION_PLACEHOLDER)).not.toBeInTheDocument()
  })

  it('submits no restriction value when the public pool is selected', async () => {
    mockedApiPost.mockResolvedValue(makeBounty())

    render(<NewBountyForm onCreated={vi.fn()} />)

    fireEvent.change(screen.getByPlaceholderText(TITLE_PLACEHOLDER), { target: { value: '公开池的单' } })

    // Type a restriction while the limited pool is selected, then switch back
    // to public before submitting — the typed value must not leak into the
    // submitted payload once the pool is public again.
    fireEvent.change(screen.getByDisplayValue('公开池'), { target: { value: 'RESTRICTED' } })
    fireEvent.change(screen.getByPlaceholderText(RESTRICTION_PLACEHOLDER), {
      target: { value: '需要 Bidding Engine 上下文' },
    })
    fireEvent.change(screen.getByDisplayValue('限定池'), { target: { value: 'PUBLIC' } })

    submitForm()

    await waitFor(() => expect(mockedApiPost).toHaveBeenCalled())
    const [, body] = mockedApiPost.mock.calls[0]
    expect((body as { restriction: string }).restriction).toBe('')
  })

  it('surfaces the server error message and keeps the typed input on a failed submission', async () => {
    mockedApiPost.mockRejectedValue(new Error('duplicate bounty title'))
    const onCreated = vi.fn()

    render(<NewBountyForm onCreated={onCreated} />)

    const titleInput = screen.getByPlaceholderText(TITLE_PLACEHOLDER)
    fireEvent.change(titleInput, { target: { value: '重复标题' } })

    submitForm()

    await waitFor(() => expect(screen.getByText('duplicate bounty title')).toBeInTheDocument())
    expect(titleInput).toHaveValue('重复标题')
    expect(onCreated).not.toHaveBeenCalled()
  })
})
