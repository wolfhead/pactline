import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import * as projectsApi from '@/api/projects'
import * as tasksApi from '@/api/tasks'
import * as workflowApi from '@/api/task-workflow'
import type { TaskThread, TaskThreadItem } from '@/task-types'
import TaskThreads from './TaskThreads'

vi.mock('@/api/projects')
vi.mock('@/api/tasks')
vi.mock('@/api/task-workflow')
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    me: { id: 'user-1', name: 'Alex', email: 'alex@example.test' },
    users: [
      { id: 'user-1', name: 'Alex', email: 'alex@example.test' },
      { id: 'user-2', name: 'Riley', email: 'riley@example.test' },
    ],
  }),
}))

const MAIN_THREAD: TaskThread = {
  id: 'thread-main',
  task_id: 'task-15',
  role: 'main',
  version: 1,
  created_at: '2026-08-20T10:00:00Z',
  updated_at: '2026-08-20T10:00:00Z',
}

const OPEN_ISSUE: TaskThread = {
  id: 'thread-open',
  task_id: 'task-15',
  role: 'issue',
  issue_type: 'decision_required',
  issue_status: 'open',
  version: 1,
  created_at: '2026-08-20T10:01:00Z',
  updated_at: '2026-08-20T10:01:00Z',
}

const RESOLVED_ISSUE: TaskThread = {
  id: 'thread-resolved',
  task_id: 'task-15',
  role: 'issue',
  issue_type: 'dependency_required',
  issue_status: 'resolved',
  version: 2,
  created_at: '2026-08-20T10:02:00Z',
  updated_at: '2026-08-20T10:03:00Z',
  resolved_at: '2026-08-20T10:03:00Z',
}

function item(overrides: Partial<TaskThreadItem> & Pick<TaskThreadItem, 'id' | 'thread_id' | 'kind' | 'created_at'>): TaskThreadItem {
  return {
    author: { type: 'user', user_id: 'user-1' },
    body: '',
    mentioned_user_ids: [],
    version: 1,
    updated_at: overrides.created_at,
    ...overrides,
  }
}

function renderThreads() {
  return render(<TaskThreads taskNumber={15} projectNumber={5} taskVersion={7} />)
}

beforeEach(() => {
  vi.mocked(projectsApi.listProjectMembers).mockResolvedValue([])
  vi.mocked(tasksApi.listActivity).mockResolvedValue([])
  vi.mocked(workflowApi.listTaskThreads).mockResolvedValue([MAIN_THREAD, OPEN_ISSUE, RESOLVED_ISSUE])
  vi.mocked(workflowApi.listThreadItems).mockImplementation(async (threadID) => {
    if (threadID === MAIN_THREAD.id) {
      return [item({
        id: 'message-owned',
        thread_id: MAIN_THREAD.id,
        kind: 'message',
        created_at: '2026-08-20T10:00:00Z',
        body: 'Original message',
      })]
    }
    if (threadID === RESOLVED_ISSUE.id) {
      return [item({
        id: 'resolved-message-owned',
        thread_id: RESOLVED_ISSUE.id,
        kind: 'message',
        created_at: '2026-08-20T10:02:00Z',
        body: 'Resolved Issue message',
      })]
    }
    return []
  })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('TaskThreads', () => {
  it('posts an explicit reply through the existing Thread reply contract', async () => {
    vi.mocked(workflowApi.createThreadMessage).mockResolvedValue(item({
      id: 'message-reply',
      thread_id: MAIN_THREAD.id,
      kind: 'message',
      created_at: '2026-08-20T10:01:00Z',
      body: 'Reply body',
      reply_to_item_id: 'message-owned',
    }))
    renderThreads()

    const article = (await screen.findByText('Original message')).closest('article')!
    fireEvent.click(within(article).getByRole('button', { name: '回复' }))
    expect(screen.getByText('正在回复：Original message')).toBeVisible()
    fireEvent.change(screen.getByRole('textbox', { name: '向当前 Thread 发送消息' }), {
      target: { value: 'Reply body' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => expect(workflowApi.createThreadMessage).toHaveBeenCalledWith(
      MAIN_THREAD.id,
      'Reply body',
      'message',
      [],
      'message-owned',
    ))
    expect(await screen.findByText('Reply body')).toBeVisible()
    expect(screen.queryByText('正在回复：Original message')).not.toBeInTheDocument()
  })

  it('keeps Main posting, progress, edit/delete, Issue switching, and resolved read-only behavior', async () => {
    const edited = item({
      id: 'message-owned',
      thread_id: MAIN_THREAD.id,
      kind: 'message',
      created_at: '2026-08-20T10:00:00Z',
      body: 'Edited message',
      version: 2,
    })
    const deleted = { ...edited, body: undefined, version: 3, deleted_at: '2026-08-20T10:04:00Z' }
    vi.mocked(workflowApi.updateThreadMessage).mockResolvedValue(edited)
    vi.mocked(workflowApi.deleteThreadMessage).mockResolvedValue(deleted)
    vi.mocked(workflowApi.createThreadMessage).mockResolvedValue(item({
      id: 'issue-reply',
      thread_id: OPEN_ISSUE.id,
      kind: 'message',
      created_at: '2026-08-20T10:05:00Z',
      body: 'Issue reply',
    }))
    renderThreads()

    const original = await screen.findByText('Original message')
    const article = original.closest('article')!
    fireEvent.click(within(article).getByRole('button', { name: '编辑' }))
    fireEvent.change(screen.getByRole('textbox', { name: '编辑消息' }), { target: { value: 'Edited message' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(workflowApi.updateThreadMessage).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'message-owned' }),
      'Edited message',
    ))
    expect(await screen.findByText('Edited message')).toBeVisible()

    fireEvent.click(within(screen.getByText('Edited message').closest('article')!).getByRole('button', { name: '删除' }))
    await waitFor(() => expect(workflowApi.deleteThreadMessage).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'message-owned', version: 2 }),
    ))
    expect(await screen.findByText('消息已删除')).toBeVisible()

    fireEvent.click(screen.getByRole('tab', { name: '待解决 · 决策 1' }))
    const issueComposer = await screen.findByRole('textbox', { name: '向当前 Thread 发送消息' })
    expect(screen.queryByLabelText('Thread Item 类型')).not.toBeInTheDocument()
    fireEvent.change(issueComposer, { target: { value: 'Issue reply' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(workflowApi.createThreadMessage).toHaveBeenCalledWith(
      OPEN_ISSUE.id,
      'Issue reply',
      'message',
    ))

    fireEvent.click(screen.getByRole('tab', { name: '已解决 · 依赖 2' }))
    const resolvedMessage = await screen.findByText('Resolved Issue message')
    const resolvedArticle = resolvedMessage.closest('article')!
    expect(within(resolvedArticle).queryByRole('button', { name: '编辑' })).not.toBeInTheDocument()
    expect(within(resolvedArticle).queryByRole('button', { name: '删除' })).not.toBeInTheDocument()
    expect(within(resolvedArticle).queryByRole('button', { name: '回复' })).not.toBeInTheDocument()
    expect(await screen.findByText('Issue 已解决，内容保持不可变；结论已合并到 Main Thread。')).toBeVisible()
    expect(screen.queryByRole('textbox', { name: '向当前 Thread 发送消息' })).not.toBeInTheDocument()
  })

  it('recovers from send, edit, and delete failures without losing the pending action', async () => {
    const edited = item({
      id: 'message-owned',
      thread_id: MAIN_THREAD.id,
      kind: 'message',
      created_at: '2026-08-20T10:00:00Z',
      body: 'Edited after retry',
      version: 2,
    })
    const deleted = { ...edited, body: undefined, version: 3, deleted_at: '2026-08-20T10:04:00Z' }
    vi.mocked(workflowApi.createThreadMessage)
      .mockRejectedValueOnce(new Error('send unavailable'))
      .mockResolvedValueOnce(item({
        id: 'progress-after-retry',
        thread_id: MAIN_THREAD.id,
        kind: 'progress',
        created_at: '2026-08-20T10:01:00Z',
        body: 'Send after retry',
      }))
    vi.mocked(workflowApi.updateThreadMessage)
      .mockRejectedValueOnce(new Error('save unavailable'))
      .mockResolvedValueOnce(edited)
    vi.mocked(workflowApi.deleteThreadMessage)
      .mockRejectedValueOnce(new Error('delete unavailable'))
      .mockResolvedValueOnce(deleted)
    renderThreads()

    const composer = await screen.findByRole('textbox', { name: '向当前 Thread 发送消息' })
    fireEvent.change(screen.getByLabelText('Thread Item 类型'), { target: { value: 'progress' } })
    fireEvent.change(composer, { target: { value: 'Send after retry' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('发送失败：send unavailable')
    expect(composer).toHaveValue('Send after retry')
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    expect(await screen.findByText('Send after retry')).toBeVisible()
    expect(composer).toHaveValue('')
    expect(workflowApi.createThreadMessage).toHaveBeenLastCalledWith(
      MAIN_THREAD.id,
      'Send after retry',
      'progress',
    )

    const originalArticle = screen.getByText('Original message').closest('article')!
    fireEvent.click(within(originalArticle).getByRole('button', { name: '编辑' }))
    const editComposer = screen.getByRole('textbox', { name: '编辑消息' })
    fireEvent.change(editComposer, { target: { value: 'Edited after retry' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('保存失败：save unavailable')
    expect(editComposer).toHaveValue('Edited after retry')
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(screen.queryByRole('textbox', { name: '编辑消息' })).not.toBeInTheDocument())
    expect(screen.getByText('Edited after retry')).toBeVisible()

    const editedArticle = screen.getByText('Edited after retry').closest('article')!
    fireEvent.click(within(editedArticle).getByRole('button', { name: '删除' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('删除失败：delete unavailable')
    expect(screen.getByText('Edited after retry')).toBeVisible()
    fireEvent.click(within(editedArticle).getByRole('button', { name: '删除' }))
    expect(await screen.findByText('消息已删除')).toBeVisible()

    expect(workflowApi.createThreadMessage).toHaveBeenCalledTimes(2)
    expect(workflowApi.updateThreadMessage).toHaveBeenCalledTimes(2)
    expect(workflowApi.deleteThreadMessage).toHaveBeenCalledTimes(2)
  })

  it('keeps activity available when the Thread source fails and retries only the failed source', async () => {
    vi.mocked(workflowApi.listTaskThreads)
      .mockRejectedValueOnce(new Error('thread unavailable'))
      .mockResolvedValueOnce([MAIN_THREAD])
    vi.mocked(tasksApi.listActivity).mockResolvedValue([{
      id: 'activity-priority',
      actor_id: 'user-2',
      field: 'priority',
      old_value: 'low',
      new_value: 'high',
      authentication_method: 'session',
      created_at: '2026-08-20T10:00:00Z',
    }])
    renderThreads()

    expect(await screen.findByText('将优先级从「低」改为「高」')).toBeVisible()
    expect(screen.getByRole('alert')).toHaveTextContent('Thread 列表加载失败')
    expect(tasksApi.listActivity).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '重试 Thread' }))
    expect(await screen.findByRole('tab', { name: 'Main' })).toBeVisible()
    expect(workflowApi.listTaskThreads).toHaveBeenCalledTimes(2)
    expect(tasksApi.listActivity).toHaveBeenCalledTimes(1)
  })

  it('keeps Thread discussion available when activity fails and recovers without polling either source', async () => {
    vi.mocked(tasksApi.listActivity)
      .mockRejectedValueOnce(new Error('activity unavailable'))
      .mockResolvedValueOnce([])
    renderThreads()

    expect(await screen.findByText('Original message')).toBeVisible()
    expect(screen.getByRole('alert')).toHaveTextContent('任务变更加载失败')
    expect(workflowApi.listTaskThreads).toHaveBeenCalledTimes(1)
    expect(tasksApi.listActivity).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '重试任务变更' }))
    await waitFor(() => expect(screen.queryByText(/任务变更加载失败/)).not.toBeInTheDocument())
    expect(tasksApi.listActivity).toHaveBeenCalledTimes(2)
    expect(workflowApi.listTaskThreads).toHaveBeenCalledTimes(1)
  })

  it('keeps activity visible when selected Thread content fails and retries its items', async () => {
    vi.mocked(workflowApi.listThreadItems)
      .mockRejectedValueOnce(new Error('items unavailable'))
      .mockResolvedValueOnce([])
    vi.mocked(tasksApi.listActivity).mockResolvedValue([{
      id: 'activity-title',
      actor_id: 'user-2',
      field: 'title',
      old_value: 'Old title',
      new_value: 'New title',
      created_at: '2026-08-20T10:00:00Z',
    }])
    renderThreads()

    expect(await screen.findByText('将标题从「Old title」改为「New title」')).toBeVisible()
    expect(screen.getByRole('alert')).toHaveTextContent('Thread 内容加载失败')
    fireEvent.click(screen.getByRole('button', { name: '重试内容' }))
    await waitFor(() => expect(workflowApi.listThreadItems).toHaveBeenCalledTimes(2))
    expect(tasksApi.listActivity).toHaveBeenCalledTimes(1)
  })
})
