import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskDetail from './TaskDetail'
import * as acceptanceApi from '@/api/acceptance'
import * as tasksApi from '@/api/tasks'
import * as projectsApi from '@/api/projects'
import type { Task } from '@/task-types'
import { ProblemError } from '@/api/v1/client'

vi.mock('@/api/tasks')
vi.mock('@/api/projects')
vi.mock('@/api/acceptance')

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; see the
// identical workaround in TaskRow.test.tsx / StatusControl.test.tsx.
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const USERS = [{ id: 'u1', name: '张沁', email: 'a@example.test' }]
const TASK = {
  id: 'id-142', number: 142, version: 1, title: '修复竞价超时导致的丢量',
  context: '竞价请求近期频繁超时', expected_result: '恢复稳定流量且不再发生异常丢量',
  description: '丢量比例升到 4.2%', status: 'in_progress' as const,
  priority: 'high' as const, assignee: USERS[0], creator: USERS[0],
  start_date: null,
  due_date: '2026-07-30', project: { id: 'p1', number: 12, name: 'Task Manager' },
  milestone: null, labels: [], parent: null, children: [],
  dependencies: [], dependents: [], blocked: false, created_at: '', updated_at: '',
  completed_at: null, archived_at: null,
}

beforeEach(() => {
  vi.mocked(tasksApi.getTask).mockResolvedValue(TASK)
  vi.mocked(tasksApi.listComments).mockResolvedValue([])
  vi.mocked(tasksApi.listTaskAgentConversations).mockResolvedValue([])
  vi.mocked(tasksApi.listActivity).mockResolvedValue([])
  vi.mocked(tasksApi.listLabels).mockResolvedValue([])
  vi.mocked(projectsApi.listProjects).mockResolvedValue([])
  vi.mocked(projectsApi.getProject).mockRejectedValue(new Error('Project detail is not needed in this test'))
  vi.mocked(acceptanceApi.listTaskCriteria).mockResolvedValue([])
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

  it('aligns labels and values on one shared property grid', async () => {
    const { container } = renderDetail()
    await screen.findByText('修复竞价超时导致的丢量')
    expect(container.querySelector('[data-task-properties]'))
      .toHaveClass('grid-cols-[5rem_minmax(0,1fr)]')
  })

  it('renders the shared acceptance checklist for tasks', async () => {
    vi.mocked(acceptanceApi.listTaskCriteria).mockResolvedValue([{
      id: 'criterion-1',
      version: 1,
      criterion: '结果可被观察',
      verification_instructions: '运行任务工作流测试',
      revision: 1,
      position: 0,
      current_check: null,
    }])
    renderDetail()
    await screen.findByText('结果可被观察')
    expect(screen.getByRole('region', { name: '验收标准' })).toBeVisible()
  })

  it('keeps Agent questions in the communication timeline and resumes explicitly', async () => {
    const claim = {
      id: 'claim-1',
      task_id: TASK.id,
      task_number: TASK.number,
      claimed_by_user_id: USERS[0].id,
      token_name: 'Codex worker',
      client_kind: 'codex',
      status: 'waiting_human' as const,
      version: 2,
      expires_at: '2026-08-01T00:00:00Z',
      created_at: '2026-07-31T00:00:00Z',
      updated_at: '2026-07-31T00:01:00Z',
    }
    const question = {
      id: 'message-1',
      claim_id: claim.id,
      task_id: TASK.id,
      author_type: 'agent' as const,
      kind: 'question' as const,
      body: '需要兼容哪个稳定版本？',
      token_name: 'Codex worker',
      created_at: '2026-07-31T00:01:00Z',
    }
    vi.mocked(tasksApi.listTaskAgentConversations)
      .mockResolvedValueOnce([{ claim, messages: [question] }])
      .mockResolvedValueOnce([{
        claim: { ...claim, status: 'active', version: 3 },
        messages: [question, {
          id: 'message-2',
          claim_id: claim.id,
          task_id: TASK.id,
          author_type: 'human',
          kind: 'answer',
          body: '兼容当前支持的稳定版本。',
          reply_to_message_id: question.id,
          created_at: '2026-07-31T00:02:00Z',
        }],
      }])
    vi.mocked(tasksApi.answerTaskClaimQuestion).mockResolvedValue({
      ...claim, status: 'active', version: 3,
    })

    renderDetail()
    await screen.findByText('需要兼容哪个稳定版本？')
    fireEvent.change(screen.getByLabelText('回复 Agent 并恢复此任务'), {
      target: { value: '兼容当前支持的稳定版本。' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送回复' }))

    await waitFor(() => expect(tasksApi.answerTaskClaimQuestion).toHaveBeenCalledWith(
      claim.id, claim.version, '兼容当前支持的稳定版本。',
    ))
    expect(await screen.findByText('兼容当前支持的稳定版本。')).toBeVisible()
  })

  it('shows an active Claim even without messages and lets the human release it', async () => {
    const claim = {
      id: 'claim-active',
      task_id: TASK.id,
      task_number: TASK.number,
      claimed_by_user_id: USERS[0].id,
      token_name: 'Codex worker',
      client_kind: 'codex',
      status: 'active' as const,
      version: 1,
      expires_at: '2026-08-07T00:00:00Z',
      created_at: '2026-07-31T00:00:00Z',
      updated_at: '2026-07-31T00:00:00Z',
    }
    vi.mocked(tasksApi.listTaskAgentConversations)
      .mockResolvedValueOnce([{ claim, messages: [] }])
      .mockResolvedValueOnce([{
        claim: {
          ...claim,
          status: 'released',
          version: 2,
          completed_at: '2026-07-31T00:05:00Z',
        },
        messages: [],
      }])
    vi.mocked(tasksApi.releaseTaskClaim).mockResolvedValue({
      ...claim,
      status: 'released',
      version: 2,
      completed_at: '2026-07-31T00:05:00Z',
    })

    renderDetail()
    expect(await screen.findByText('Agent 正在执行')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '释放 Claim' }))

    await waitFor(() => expect(tasksApi.releaseTaskClaim).toHaveBeenCalledWith(
      claim.id, claim.version,
    ))
    await waitFor(() => expect(screen.queryByText('Agent 正在执行')).not.toBeInTheDocument())
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

  it('refreshes the task and warns when a concurrent Agent changed it', async () => {
    const latest = { ...TASK, version: 2, status: 'todo' as const }
    vi.mocked(tasksApi.updateTask).mockRejectedValue(
      new ProblemError(412, 'VERSION_CONFLICT', 'req-conflict', 2),
    )
    vi.mocked(tasksApi.getTask)
      .mockResolvedValueOnce(TASK)
      .mockResolvedValueOnce(latest)
    const onPatched = vi.fn()
    renderDetail({ onPatched })
    await screen.findByText('修复竞价超时导致的丢量')

    fireEvent.click(screen.getByRole('combobox', { name: '状态' }))
    fireEvent.click(await screen.findByRole('option', { name: '已完成' }))

    expect(await screen.findByText('内容已被其他用户或 Agent 更新，已加载最新版本。'))
      .toBeVisible()
    expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('待办')
    expect(onPatched).toHaveBeenCalledWith(latest)
    expect(tasksApi.updateTask).toHaveBeenCalledWith(142, 1, { status: 'done' })
  })

  // At xl the `number` prop changes in place — clicking another row in the
  // list column never remounts TaskDetail — so anything resolving from the
  // task that just left has to be dropped rather than written into state.
  describe('when `number` changes without a remount', () => {
    const OTHER = {
      ...TASK,
      id: 'id-143',
      number: 143,
      title: '另一条任务',
      status: 'todo' as const,
    }

    beforeEach(() => {
      vi.mocked(tasksApi.getTask).mockImplementation((n: number) =>
        Promise.resolve(n === OTHER.number ? OTHER : TASK),
      )
    })

    it('drops a patch response that belongs to the task that just left', async () => {
      const onPatched = vi.fn()
      let resolvePatch!: (task: Task) => void
      vi.mocked(tasksApi.updateTask).mockReturnValue(
        new Promise((resolve) => {
          resolvePatch = resolve
        }),
      )

      const { rerender } = render(
        <MemoryRouter>
          <TaskDetail number={142} users={USERS} onPatched={onPatched} />
        </MemoryRouter>,
      )
      await screen.findByText('修复竞价超时导致的丢量')

      // Commit a change on #142, leaving the request in flight.
      fireEvent.click(screen.getByRole('combobox', { name: '状态' }))
      fireEvent.click(await screen.findByRole('option', { name: '已完成' }))
      expect(vi.mocked(tasksApi.updateTask)).toHaveBeenCalledWith(
        142, 1, { status: 'done' },
      )

      // Now #143 is selected and loads — same component instance.
      rerender(
        <MemoryRouter>
          <TaskDetail number={143} users={USERS} onPatched={onPatched} />
        </MemoryRouter>,
      )
      await screen.findByText('另一条任务')

      // ... and only then does #142's PATCH come back.
      resolvePatch({ ...TASK, status: 'done' as const })
      // onPatched still fires — the list wants the change regardless of
      // what the detail happens to be showing — and gives a deterministic
      // point at which the response has definitely been handled.
      await waitFor(() => expect(onPatched).toHaveBeenCalled())

      // The pane must still be #143's. Writing #142's response here would
      // put its title, status and comments under the URL /tasks/143 with
      // nothing left to correct it.
      expect(screen.getByRole('textbox', { name: '任务标题' })).toHaveValue('另一条任务')
      expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('待办')
      expect(screen.queryByText('修复竞价超时导致的丢量')).not.toBeInTheDocument()
    })

    it('drops the undo toast, so 撤销 cannot restore a task the user has left', async () => {
      vi.mocked(tasksApi.archiveTask).mockResolvedValue({
        ...TASK,
        archived_at: '2026-07-27T00:00:00Z',
      })

      const { rerender } = render(
        <MemoryRouter>
          <TaskDetail number={142} users={USERS} onPatched={() => {}} />
        </MemoryRouter>,
      )
      await screen.findByText('修复竞价超时导致的丢量')

      fireEvent.click(screen.getByRole('button', { name: '归档' }))
      await screen.findByText('已归档任务。')

      rerender(
        <MemoryRouter>
          <TaskDetail number={143} users={USERS} onPatched={() => {}} />
        </MemoryRouter>,
      )
      await screen.findByText('另一条任务')

      // The toast belongs to #142. Left up, it reads as #143's outcome, and
      // its undo button still closes over restoreTask(142) — one click away
      // from silently un-archiving a task the user is no longer looking at.
      expect(screen.queryByText('已归档任务。')).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '撤销' })).not.toBeInTheDocument()
    })
  })
})
