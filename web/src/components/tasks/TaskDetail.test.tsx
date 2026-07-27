import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskDetail from './TaskDetail'
import * as tasksApi from '@/api/tasks'

vi.mock('@/api/tasks')

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; see the
// identical workaround in TaskRow.test.tsx / StatusControl.test.tsx.
afterEach(() => {
  cleanup()
})

const USERS = [{ id: 'u1', name: '张沁', email: 'a@x.com' }]
const TASK = {
  id: 'id-142', number: 142, title: '修复竞价超时导致的丢量',
  description: '丢量比例升到 4.2%', status: 'in_progress' as const,
  priority: 'high' as const, assignee: USERS[0], creator: USERS[0],
  due_date: '2026-07-30', labels: [], created_at: '', updated_at: '',
  completed_at: null, archived_at: null,
}

beforeEach(() => {
  vi.mocked(tasksApi.getTask).mockResolvedValue(TASK)
  vi.mocked(tasksApi.listComments).mockResolvedValue([])
  vi.mocked(tasksApi.listActivity).mockResolvedValue([])
  vi.mocked(tasksApi.listLabels).mockResolvedValue([])
})

function renderDetail(props: Partial<React.ComponentProps<typeof TaskDetail>> = {}) {
  return render(
    <MemoryRouter>
      <TaskDetail number={142} users={USERS} onPatched={() => {}} {...props} />
    </MemoryRouter>,
  )
}

describe('TaskDetail', () => {
  it('renders no shell of its own — no dialog, no page chrome', async () => {
    renderDetail()
    await screen.findByText('修复竞价超时导致的丢量')
    // The shell is the caller's job. A TaskDetail that mounts its own Sheet
    // would render twice over inside the three-column layout.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('uses the bare property labels, not the row-scoped ones', async () => {
    renderDetail()
    await screen.findByText('修复竞价超时导致的丢量')
    expect(screen.getByRole('combobox', { name: '状态' })).toBeVisible()
    expect(screen.queryByRole('combobox', { name: /任务 #142 状态/ })).not.toBeInTheDocument()
  })

  it('tells the caller about a change so the list can follow it', async () => {
    const onPatched = vi.fn()
    const patched = { ...TASK, status: 'done' as const }
    vi.mocked(tasksApi.updateTask).mockResolvedValue(patched)
    renderDetail({ onPatched })
    await screen.findByText('修复竞价超时导致的丢量')

    const { fireEvent } = await import('@testing-library/react')
    fireEvent.click(screen.getByRole('combobox', { name: '状态' }))
    fireEvent.click(await screen.findByRole('option', { name: '已完成' }))

    // This is what keeps the list column in step with the detail column.
    await waitFor(() => expect(onPatched).toHaveBeenCalledWith(patched))
  })

  it('shows a close affordance only when the shell provides one', async () => {
    const { unmount } = renderDetail()
    await screen.findByText('修复竞价超时导致的丢量')
    expect(screen.queryByRole('button', { name: '关闭' })).not.toBeInTheDocument()
    unmount()

    renderDetail({ onClose: () => {} })
    await screen.findByText('修复竞价超时导致的丢量')
    expect(screen.getByRole('button', { name: '关闭' })).toBeVisible()
  })
})
