import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TaskMergeRequests from './TaskMergeRequests'
import { getTaskDelivery, linkTaskMergeRequest, unlinkTaskMergeRequest } from '@/api/task-delivery'
import { listTaskStageClaims } from '@/api/task-workflow'
import type { Task, TaskStageClaim } from '@/task-types'

vi.mock('@/api/task-delivery', () => ({
  getTaskDelivery: vi.fn(),
  linkTaskMergeRequest: vi.fn(),
  unlinkTaskMergeRequest: vi.fn(),
}))
vi.mock('@/api/task-workflow', () => ({ listTaskStageClaims: vi.fn() }))
vi.mock('@/identity', () => ({ useIdentity: () => ({ me: { id: 'user-1' } }) }))

const TASK = { number: 18, version: 7, phase: 'in_progress' } as Task
const CLAIM = {
  id: 'claim-1', version: 2, status: 'active', stage: 'execution',
  claimed_by: { type: 'user', user_id: 'user-1' },
} as TaskStageClaim
const OBSERVATION = {
  status: 'confirmed' as const,
  observed_at: '2026-08-13T08:00:00Z',
  title: 'Deliver evidence', state: 'opened' as const, draft: false,
  source_branch: 'feature/evidence', target_branch: 'main', head_sha: 'abcdef123456',
  provider_updated_at: '2026-08-13T08:00:00Z',
}
const LINK = {
  id: 'link-1', project_repository_id: 'repo-1',
  repository_url: 'https://gitlab.example/team/app', merge_request_iid: 42,
  gitlab_merge_request_id: 91,
  web_url: 'https://gitlab.example/team/app/-/merge_requests/42',
  linked_by: { type: 'user' as const, user_id: 'user-1' },
  linked_through_claim_id: 'claim-1', linked_at: '2026-08-13T08:00:00Z',
  latest_observation: OBSERVATION,
}

describe('TaskMergeRequests', () => {
  afterEach(cleanup)
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getTaskDelivery).mockResolvedValue({ active_links: [] })
    vi.mocked(listTaskStageClaims).mockResolvedValue([CLAIM])
    vi.mocked(linkTaskMergeRequest).mockResolvedValue({
      task: { task_id: 'task-1', task_number: 18, version: 8, phase: 'in_progress', activity: 'working', review_cycle: 0, main_thread_id: 'thread-1' },
      merge_request: LINK,
    })
  })

  it('lets the human execution claimant link a GitLab merge request', async () => {
    const onChanged = vi.fn().mockResolvedValue(undefined)
    render(<TaskMergeRequests task={TASK} onChanged={onChanged} />)
    const input = await screen.findByLabelText('GitLab Merge Request 地址')
    fireEvent.change(input, { target: { value: LINK.web_url } })
    fireEvent.click(screen.getByRole('button', { name: '关联 MR' }))
    await waitFor(() => expect(linkTaskMergeRequest).toHaveBeenCalledWith(
      18, 7, CLAIM, LINK.web_url,
    ))
    expect(onChanged).toHaveBeenCalledOnce()
  })

  it('refreshes Claim ownership when the task workflow version changes', async () => {
    vi.mocked(listTaskStageClaims)
      .mockResolvedValueOnce([])
      .mockResolvedValue([CLAIM])
    const { rerender } = render(<TaskMergeRequests task={TASK} onChanged={vi.fn()} />)
    await waitFor(() => expect(listTaskStageClaims).toHaveBeenCalledTimes(1))
    expect(screen.queryByLabelText('GitLab Merge Request 地址')).not.toBeInTheDocument()

    rerender(<TaskMergeRequests task={{ ...TASK, version: 8 } as Task} onChanged={vi.fn()} />)

    expect(await screen.findByLabelText('GitLab Merge Request 地址')).toBeInTheDocument()
    expect(listTaskStageClaims).toHaveBeenCalledTimes(2)
  })

  it('preserves an unlink failure after refreshing current delivery state', async () => {
    vi.mocked(getTaskDelivery).mockResolvedValue({ active_links: [LINK] })
    vi.mocked(unlinkTaskMergeRequest).mockRejectedValue(new Error('unlink rejected'))
    render(<TaskMergeRequests task={TASK} onChanged={vi.fn().mockResolvedValue(undefined)} />)

    fireEvent.click(await screen.findByRole('button', { name: '取消关联' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('unlink rejected')
    expect(getTaskDelivery).toHaveBeenCalledTimes(2)
  })

  it('does not present a loading failure as an empty delivery', async () => {
    vi.mocked(getTaskDelivery).mockRejectedValue(new Error('delivery unavailable'))
    render(<TaskMergeRequests task={TASK} onChanged={vi.fn()} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('delivery unavailable')
    expect(screen.queryByText('当前没有关联 Merge Request。')).not.toBeInTheDocument()
  })

  it('shows the frozen review SHA beside current GitLab state without edit controls', async () => {
    vi.mocked(listTaskStageClaims).mockResolvedValue([])
    vi.mocked(getTaskDelivery).mockResolvedValue({
      active_links: [{ ...LINK, latest_observation: { ...OBSERVATION, head_sha: 'fedcba987654' } }],
      review: {
        review_cycle: 2,
        merge_requests: [{
          comparison: 'moved',
          current: { ...LINK, latest_observation: { ...OBSERVATION, head_sha: 'fedcba987654' } },
          snapshot: {
            task_merge_request_id: 'link-1', project_repository_id: 'repo-1',
            connection_id: 'connection-1', gitlab_project_id: 42, merge_request_iid: 42,
            web_url: LINK.web_url, title: 'Deliver evidence', state: 'opened', draft: false,
            source_branch: 'feature/evidence', target_branch: 'main', head_sha: 'abcdef123456',
            observation_status: 'confirmed', observed_at: '2026-08-13T08:00:00Z',
          },
        }],
      },
    })
    render(<TaskMergeRequests task={{ ...TASK, phase: 'in_review' } as Task} onChanged={vi.fn()} />)
    expect(await screen.findByText('提交后有新提交')).toBeInTheDocument()
    expect(screen.getByText(/冻结 打开 \/ abcdef12 · 当前 打开 \/ fedcba98/)).toBeInTheDocument()
    expect(screen.getAllByText(/gitlab\.example\/team\/app · !42/)).toHaveLength(2)
    expect(screen.queryByLabelText('GitLab Merge Request 地址')).not.toBeInTheDocument()
  })
})
