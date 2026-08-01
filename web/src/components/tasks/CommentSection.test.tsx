import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import CommentSection from './CommentSection'
import * as tasksApi from '@/api/tasks'
import * as projectsApi from '@/api/projects'
import type { Comment } from '@/task-types'

vi.mock('@/api/tasks')
vi.mock('@/api/projects')
vi.mock('@/identity', () => ({
  useIdentity: () => ({ me: { id: 'u1', name: 'Alex' } }),
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const ROOT: Comment = {
  id: 'comment-1', task_id: 'task-1', author_id: 'u2', version: 1,
  body: 'Can someone follow up?', thread_root_id: 'comment-1',
  mentioned_user_ids: [], deleted: false, created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z',
}

describe('CommentSection', () => {
  beforeEach(() => {
    vi.mocked(tasksApi.listComments).mockResolvedValue([ROOT])
    vi.mocked(tasksApi.listTaskAgentConversations).mockResolvedValue([])
    vi.mocked(projectsApi.listProjectMembers).mockResolvedValue([
      {
        id: 'm1', project_id: 'project-1', role: 'admin', active: true,
        user: { id: 'u1', name: 'Alex', email: null }, created_at: '', updated_at: '',
      },
      {
        id: 'm2', project_id: 'project-1', role: 'member', active: true,
        user: { id: 'u2', name: 'Blair', email: null }, created_at: '', updated_at: '',
      },
    ])
    vi.mocked(tasksApi.createComment).mockResolvedValue({
      ...ROOT, id: 'comment-2', author_id: 'u1', body: 'I will handle it',
      reply_to_comment_id: ROOT.id, mentioned_user_ids: ['u2'],
    })
  })

  it('submits an exact reply target and structured Project-member mention', async () => {
    render(
      <CommentSection
        taskNumber={42}
        projectNumber={12}
        taskVersion={5}
        taskStatus="in_progress"
        acceptanceCriteria={[]}
        onReviewCheck={vi.fn()}
        onCompleteReview={vi.fn()}
        onReturnForChanges={vi.fn()}
        onTaskChanged={vi.fn().mockResolvedValue(undefined)}
      />,
    )
    await screen.findByText('Can someone follow up?')
    fireEvent.click(screen.getByRole('button', { name: '回复' }))
    fireEvent.mouseDown(screen.getByRole('button', { name: '提及项目成员' }))
    fireEvent.mouseDown(screen.getByRole('option', { name: /Blair/ }))
    const editor = screen.getByRole('combobox', { name: '新评论内容' })
    editor.append(document.createTextNode('I will handle it'))
    fireEvent.input(editor)
    fireEvent.click(screen.getByRole('button', { name: '评论' }))

    await waitFor(() => expect(tasksApi.createComment).toHaveBeenCalledWith(
      42, 5, '@Blair I will handle it', 'comment-1', ['u2'],
    ))
  })
})
