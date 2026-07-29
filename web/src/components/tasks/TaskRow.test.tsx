import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskRow from './TaskRow'
import type { Task } from '@/task-types'

const USERS = [
  { id: 'u1', name: '张沁', email: 'a@x.com' },
  { id: 'u2', name: '王溪', email: 'b@x.com' },
]
const TASK: Task = {
  id: 'id-142', number: 142, version: 1,
  title: '修复竞价超时导致的丢量',
  context: '竞价请求近期频繁超时', expected_result: '恢复稳定流量',
  description: '',
  status: 'todo', priority: 'high', assignee: USERS[0],
  creator: USERS[0], due_date: '2026-07-30',
  project: { id: 'p1', number: 12, name: 'Task Manager' }, milestone: null, labels: [],
  created_at: '', updated_at: '', completed_at: null, archived_at: null,
}

function renderRow(over: Partial<Task> = {}, onPatch = vi.fn()) {
  render(
    <MemoryRouter>
      <TaskRow task={{ ...TASK, ...over }} selected={false} tier="xl" users={USERS}
        onPatch={onPatch} onArchive={() => {}} onRestore={() => {}} />
    </MemoryRouter>,
  )
  return onPatch
}

describe('TaskRow', () => {
  // vitest.config's test block doesn't set `globals: true`, so
  // @testing-library/react's own auto-cleanup never registers; see the
  // identical workaround in StatusControl.test.tsx / AssigneeControl.test.tsx.
  afterEach(() => {
    cleanup()
  })

  it('shows all three controls without any hover or click first', () => {
    renderRow()
    // This is the whole point of the rewrite: the old QuietSelect only
    // rendered a control after interaction. A regression to that fails here.
    expect(screen.getByRole('combobox', { name: '任务 #142 状态' })).toBeVisible()
    expect(screen.getByRole('combobox', { name: '任务 #142 优先级' })).toBeVisible()
    expect(screen.getByRole('combobox', { name: '任务 #142 负责人' })).toBeVisible()
  })

  it('sends an optimistic patch carrying both the wire value and the display value', async () => {
    const onPatch = renderRow()
    fireEvent.click(screen.getByRole('combobox', { name: '任务 #142 状态' }))
    fireEvent.click(await screen.findByRole('option', { name: '进行中' }))
    expect(onPatch).toHaveBeenCalledWith(
      expect.objectContaining({ number: 142 }),
      { status: 'in_progress' },
      { status: 'in_progress' },
    )
  })

  it('maps an assignee change to both the id patch and the resolved user object', async () => {
    const onPatch = renderRow()
    fireEvent.click(screen.getByRole('combobox', { name: '任务 #142 负责人' }))
    fireEvent.click(await screen.findByRole('option', { name: '王溪' }))
    // The optimistic half must be the resolved UserRef, not the bare id —
    // the row renders assignee.name, so an id here would blank the cell
    // until the server answered.
    expect(onPatch).toHaveBeenCalledWith(
      expect.objectContaining({ number: 142 }),
      { assignee_id: 'u2' },
      { assignee: USERS[1] },
    )
  })

  it('links the title to the task', () => {
    renderRow()
    expect(screen.getByRole('link', { name: '修复竞价超时导致的丢量' }))
      .toHaveAttribute('href', '/tasks/142')
  })

  const LONG = { ...TASK, number: 143, title: '把竞价链路端到端的 P99 压到 50ms 以下并补齐全链路埋点' }

  it('gives the phone row a second line for the metadata', () => {
    render(
      <MemoryRouter>
        <TaskRow task={LONG} selected={false} tier="phone" users={USERS}
          onPatch={vi.fn()} onArchive={() => {}} onRestore={() => {}} />
      </MemoryRouter>,
    )
    // The phone row is two lines: the title owns the first one outright, so
    // it is NOT a sibling of the metadata. Decoy this catches: reusing the
    // desktop single-line row, where title and metadata share one flex row.
    const title = screen.getByRole('link', { name: LONG.title })
    const due = screen.getByText('7月30日')
    expect(title.parentElement).not.toBe(due.parentElement)
  })

  it('renders the desktop row as a single line', () => {
    render(
      <MemoryRouter>
        <TaskRow task={LONG} selected={false} tier="xl" users={USERS}
          onPatch={vi.fn()} onArchive={() => {}} onRestore={() => {}} />
      </MemoryRouter>,
    )
    const title = screen.getByRole('link', { name: LONG.title })
    // The title sits directly on the row's own flex line, beside the
    // fixed-width control columns — it is not wrapped in a block of its
    // own the way the phone row's first line wraps it. Asserting that
    // rather than "shares a parent with the due date": the due date is now
    // column-wrapped like every other control, and which controls happen
    // to be nested one div deeper is not what "single line" means.
    expect(title.parentElement).toBe(screen.getByRole('listitem'))
  })
})
