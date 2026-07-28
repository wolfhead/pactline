import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import { createRef } from 'react'
import InlineCreate from './InlineCreate'
import * as tasksApi from '@/api/tasks'

vi.mock('@/api/tasks')

const CREATED = {
  id: 'id-9', number: 9, title: '新记一条', description: '',
  status: 'backlog' as const, priority: 'none' as const, assignee: null,
  creator: { id: 'u1', name: '张沁', email: 'a@x.com' },
  due_date: null, project: null, milestone: null, labels: [], created_at: '', updated_at: '',
  completed_at: null, archived_at: null,
}

beforeEach(() => {
  vi.mocked(tasksApi.createTask).mockResolvedValue(CREATED)
})

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers.
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('InlineCreate', () => {
  it('creates from a title alone — no other field is required', async () => {
    const onCreated = vi.fn()
    render(<InlineCreate onCreated={onCreated} />)
    const input = screen.getByRole('textbox', { name: '新建任务' })
    fireEvent.change(input, { target: { value: '新记一条' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(tasksApi.createTask).toHaveBeenCalledWith({ title: '新记一条' }))
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(CREATED))
  })

  it('clears and keeps focus so a second one can be typed straight away', async () => {
    render(<InlineCreate onCreated={() => {}} />)
    const input = screen.getByRole('textbox', { name: '新建任务' }) as HTMLInputElement
    fireEvent.change(input, { target: { value: '新记一条' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    // Capturing several thoughts in a row is the whole point of the row
    // staying on screen. A decoy that blurs or keeps the text fails here.
    await waitFor(() => expect(input.value).toBe(''))
    expect(document.activeElement).toBe(input)
  })

  it('does not fire on a blank or whitespace-only title', () => {
    render(<InlineCreate onCreated={() => {}} />)
    const input = screen.getByRole('textbox', { name: '新建任务' })
    fireEvent.keyDown(input, { key: 'Enter' })
    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(tasksApi.createTask).not.toHaveBeenCalled()
  })

  it('exposes focus() so the toolbar button can reach it', () => {
    const ref = createRef<{ focus(): void }>()
    render(<InlineCreate ref={ref} onCreated={() => {}} />)
    ref.current!.focus()
    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: '新建任务' }))
  })
})
