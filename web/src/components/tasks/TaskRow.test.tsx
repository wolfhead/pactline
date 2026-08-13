import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskRow from './TaskRow'
import type { Task } from '@/task-types'

const USERS = [
  { id: 'u1', name: '张沁', email: 'a@example.test' },
  { id: 'u2', name: '王溪', email: 'b@example.test' },
]
const TASK: Task = {
  id: 'id-142', number: 142, version: 1,
  title: '修复竞价超时导致的丢量',
  context: '竞价请求近期频繁超时', expected_result: '恢复稳定流量',
  description: '',
  phase: 'backlog', activity: null, review_cycle: 0, main_thread_id: 'thread-main-142',
  priority: 'high', assignee: USERS[0],
  creator: USERS[0], start_date: null, due_date: '2026-07-30',
  project: { id: 'p1', number: 12, name: 'Task Manager' }, milestone: null, labels: [],
  parent: null, children: [], dependencies: [], dependents: [], blocked: false,
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

  it('shows phase plus editable priority and assignee without hover', () => {
    renderRow()
    // This is the whole point of the rewrite: the old QuietSelect only
    // rendered a control after interaction. A regression to that fails here.
    expect(screen.getByRole('status', { name: '待规划' })).toBeVisible()
    expect(screen.getByRole('combobox', { name: '任务 #142 优先级' })).toBeVisible()
    expect(screen.getByRole('combobox', { name: '任务 #142 负责人' })).toBeVisible()
  })

  it('shows the actor-neutral working activity inside the phase marker', () => {
    renderRow({
      phase: 'in_progress',
      activity: 'working',
    })

    expect(screen.getByRole('status', { name: '执行中 · 正在处理' })).toBeVisible()
    expect(screen.queryByRole('combobox', { name: /状态/ })).not.toBeInTheDocument()
  })

  it('shows needs_resolution without encoding who requested help', () => {
    render(
      <MemoryRouter>
        <TaskRow
          task={{
            ...TASK,
            phase: 'in_progress',
            activity: 'needs_resolution',
          }}
          selected={false}
          tier="xl"
          users={USERS}
          onPatch={vi.fn()}
          onArchive={() => {}}
          onRestore={() => {}}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('status', { name: '执行中 · 等待解决' })).toBeVisible()
  })

  it('does not expose phase as an arbitrary optimistic patch', () => {
    const onPatch = renderRow()
    expect(screen.queryByRole('combobox', { name: /状态|阶段/ })).not.toBeInTheDocument()
    expect(onPatch).not.toHaveBeenCalled()
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
