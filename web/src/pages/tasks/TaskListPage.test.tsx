import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import TaskListPage from './TaskListPage'
import * as tasksApi from '@/api/tasks'

vi.mock('@/api/tasks')
vi.mock('@/identity', async () => ({
  ...(await vi.importActual<typeof import('@/identity')>('@/identity')),
  useIdentity: () => ({
    me: { id: 'u1', name: '张沁', email: 'a@x.com' },
    users: [{ id: 'u1', name: '张沁', email: 'a@x.com' }],
    switchTo: () => {},
  }),
}))

function setWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: px })
  window.dispatchEvent(new Event('resize'))
}

const TASK = {
  id: 'id-142', number: 142, title: '修复竞价超时', description: '',
  status: 'todo' as const, priority: 'none' as const, assignee: null,
  creator: { id: 'u1', name: '张沁', email: 'a@x.com' },
  due_date: null, labels: [], created_at: '', updated_at: '',
  completed_at: null, archived_at: null,
}

beforeEach(() => {
  vi.mocked(tasksApi.listTasks).mockResolvedValue({ items: [TASK], has_more: false })
  vi.mocked(tasksApi.listLabels).mockResolvedValue([])
  vi.mocked(tasksApi.getTask).mockResolvedValue(TASK)
  vi.mocked(tasksApi.listComments).mockResolvedValue([])
  vi.mocked(tasksApi.listActivity).mockResolvedValue([])
})

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this a
// component rendered by one test stays mounted and pollutes the next test's
// queries — which here would let every "the detail is on screen" assertion
// pass for the wrong reason.
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/:number" element={<TaskListPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('TaskListPage', () => {
  it('keeps the detail column occupying its space with nothing selected', async () => {
    setWidth(1440)
    renderAt('/tasks')
    await screen.findByText('修复竞价超时')
    // The empty state is what holds the column open. Without it the list
    // would widen on deselect and jump back on select.
    expect(screen.getByText('从左边选一条任务')).toBeVisible()
  })

  it('shows the detail in the third column, not a dialog, at xl', async () => {
    setWidth(1440)
    renderAt('/tasks/142')
    await screen.findAllByText('修复竞价超时')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('shows the detail as a dialog below xl', async () => {
    setWidth(1120)
    renderAt('/tasks/142')
    // At lg the detail slides over the list instead of taking a column.
    expect(await screen.findByRole('dialog')).toBeVisible()
    // ... over a list that is still mounted underneath it, at unchanged
    // width. Swapping the list out for the Sheet would lose scroll position
    // and filters the moment a task is opened.
    await waitFor(() => expect(screen.getAllByRole('listitem', { hidden: true }).length).toBeGreaterThan(0))
  })

  it('keeps the list mounted while the detail is open', async () => {
    setWidth(1440)
    renderAt('/tasks/142')
    // The list must still be there — that is the whole reason for three
    // columns. A decoy that swaps the list out for the detail fails here.
    await waitFor(() => expect(screen.getAllByRole('listitem').length).toBeGreaterThan(0))
  })

  it('reflects a detail-side change in the list without refetching', async () => {
    setWidth(1440)
    vi.mocked(tasksApi.updateTask).mockResolvedValue({ ...TASK, status: 'done' })
    renderAt('/tasks/142')
    await screen.findAllByText('修复竞价超时')

    const listCalls = vi.mocked(tasksApi.listTasks).mock.calls.length
    fireEvent.click(screen.getByRole('combobox', { name: '状态' }))
    fireEvent.click(await screen.findByRole('option', { name: '已完成' }))

    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: '任务 #142 状态' })).toHaveTextContent('已完成'),
    )
    // Shared state, not a re-fetch: the list must not hit the API again.
    expect(vi.mocked(tasksApi.listTasks).mock.calls.length).toBe(listCalls)
  })

  it('reflects a list-side change in the detail without refetching', async () => {
    setWidth(1440)
    vi.mocked(tasksApi.updateTask).mockResolvedValue({ ...TASK, status: 'done' })
    renderAt('/tasks/142')
    await screen.findAllByText('修复竞价超时')
    expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('待办')

    // The other direction of the same seam. At xl the selected row's
    // controls are live and sit directly beside the open detail, so a change
    // committed from the row has to move the detail too — otherwise the two
    // panes show different values for one task, side by side.
    const listCalls = vi.mocked(tasksApi.listTasks).mock.calls.length
    fireEvent.click(screen.getByRole('combobox', { name: '任务 #142 状态' }))
    fireEvent.click(await screen.findByRole('option', { name: '已完成' }))

    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('已完成'),
    )
    expect(vi.mocked(tasksApi.listTasks).mock.calls.length).toBe(listCalls)
    // The detail must not have re-fetched the task to notice, either.
    expect(vi.mocked(tasksApi.getTask).mock.calls.length).toBe(1)
  })

  it('reverts an optimistic row change and explains why when the server refuses it', async () => {
    // Nothing selected: this is about the list column alone, which still
    // owns its own optimistic update/revert independently of the detail.
    setWidth(1440)
    renderAt('/tasks')

    // Re-queried rather than held: Radix fills a Select trigger's label by
    // portalling the selected item's text into it, so a reference captured
    // before a value change can go stale.
    const statusTrigger = () => screen.getByRole('combobox', { name: '任务 #142 状态' })

    // Status is a permanently visible combobox — no reveal-on-interaction
    // step first.
    await screen.findByRole('combobox', { name: '任务 #142 状态' })
    expect(statusTrigger()).toHaveTextContent('待办')

    let rejectPatch!: (err: Error) => void
    vi.mocked(tasksApi.updateTask).mockReturnValue(
      new Promise<never>((_resolve, reject) => {
        rejectPatch = reject
      }),
    )

    fireEvent.click(statusTrigger())
    fireEvent.click(await screen.findByRole('option', { name: '已完成' }))

    // The optimistic value is on the row immediately, before the server has
    // answered at all.
    await waitFor(() => expect(statusTrigger()).toHaveTextContent('已完成'))
    expect(vi.mocked(tasksApi.updateTask)).toHaveBeenCalledWith(142, { status: 'done' })
    expect(screen.queryByText(/已恢复原状态/)).not.toBeInTheDocument()

    rejectPatch(new Error('cannot skip directly to done'))

    // Revert: back to the value it held before, plus a message naming what
    // went wrong. A refused PATCH that silently leaves the new value on
    // screen is the failure this guards.
    await waitFor(() => expect(statusTrigger()).toHaveTextContent('待办'))
    expect(await screen.findByText(/cannot skip directly to done/)).toBeInTheDocument()
    expect(screen.getByText(/已恢复原状态/)).toBeInTheDocument()
  })
})
