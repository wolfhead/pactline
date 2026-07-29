import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import { createRef } from 'react'
import InlineCreate from './InlineCreate'
import * as tasksApi from '@/api/tasks'
import * as projectsApi from '@/api/projects'

vi.mock('@/api/tasks')
vi.mock('@/api/projects')

const CREATED = {
  id: 'id-9', number: 9, version: 1, title: '新记一条', description: '',
  status: 'todo' as const, priority: 'none' as const, assignee: null,
  creator: { id: 'u1', name: '张沁', email: 'a@x.com' },
  due_date: null, project: { id: 'p1', number: 12, name: 'Task Manager' },
  milestone: null, labels: [], created_at: '', updated_at: '',
  completed_at: null, archived_at: null,
}

beforeEach(() => {
  window.localStorage.clear()
  vi.mocked(tasksApi.createTask).mockResolvedValue(CREATED)
  vi.mocked(projectsApi.listProjects).mockResolvedValue([{
    id: 'p1', number: 12, version: 1, name: 'Task Manager', description: '',
    owner: { id: 'u1', name: '张沁', email: 'a@x.com' },
    creator: { id: 'u1', name: '张沁', email: 'a@x.com' },
    archived_at: null, created_at: '', updated_at: '', completed_tasks: 0, eligible_tasks: 0,
  }])
})

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers.
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('InlineCreate', () => {
  it('creates in the selected Project', async () => {
    const onCreated = vi.fn()
    render(<InlineCreate onCreated={onCreated} />)
    const input = screen.getByRole('textbox', { name: '新建任务' })
    await waitFor(() => expect(input).toBeEnabled())
    fireEvent.change(input, { target: { value: '新记一条' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(tasksApi.createTask).toHaveBeenCalledWith({
      title: '新记一条', project_number: 12,
    }))
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(CREATED))
  })

  it('clears and keeps focus so a second one can be typed straight away', async () => {
    render(<InlineCreate onCreated={() => {}} />)
    const input = screen.getByRole('textbox', { name: '新建任务' }) as HTMLInputElement
    await waitFor(() => expect(input).toBeEnabled())
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

  it('exposes focus() so the toolbar button can reach it', async () => {
    const ref = createRef<{ focus(): void }>()
    render(<InlineCreate ref={ref} onCreated={() => {}} />)
    await waitFor(() => expect(screen.getByRole('textbox', { name: '新建任务' })).toBeEnabled())
    ref.current!.focus()
    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: '新建任务' }))
  })

  it('honors focus requested while the Project catalog is still loading', async () => {
    let resolveProjects!: (projects: projectsApi.Project[]) => void
    vi.mocked(projectsApi.listProjects).mockReturnValue(new Promise((resolve) => {
      resolveProjects = resolve
    }))
    const ref = createRef<{ focus(): void }>()
    render(<InlineCreate ref={ref} onCreated={() => {}} />)
    const input = screen.getByRole('textbox', { name: '新建任务' })

    expect(input).toBeDisabled()
    ref.current!.focus()
    resolveProjects([{
      id: 'p1', number: 12, version: 1, name: 'Task Manager', description: '',
      owner: { id: 'u1', name: '张沁', email: 'a@x.com' },
      creator: { id: 'u1', name: '张沁', email: 'a@x.com' },
      archived_at: null, created_at: '', updated_at: '', completed_tasks: 0, eligible_tasks: 0,
    }])

    await waitFor(() => expect(input).toBeEnabled())
    expect(document.activeElement).toBe(input)
  })
})
