import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import ProjectControl from './ProjectControl'
import * as projectsApi from '@/api/projects'

vi.mock('@/api/projects')

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

beforeEach(() => {
  vi.mocked(projectsApi.listProjects).mockResolvedValue([
    {
      id: 'p1', number: 12, version: 1, name: 'Launch', description: '',
      creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
      archived_at: null, created_at: '', updated_at: '', completed_tasks: 0,
      eligible_tasks: 0,
    },
  ])
  vi.mocked(projectsApi.getProject).mockResolvedValue({
    project: (awaitProject()),
    milestones: [{
      id: 'm1', project_id: 'p1', version: 1,
      name: 'API ready', outcome: 'Ready', description: '', owner_id: 'u1', status: 'active',
      target_date: null, position: 0, completed_at: null, cancelled_at: null,
      created_at: '', updated_at: '', acceptance_criteria: [],
    }],
    tasks: [],
    activity: [],
  })
})

function awaitProject() {
  return {
    id: 'p1', number: 12, version: 1, name: 'Launch', description: '',
    creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    archived_at: null, created_at: '', updated_at: '', completed_tasks: 0,
    eligible_tasks: 0,
  }
}

describe('ProjectControl', () => {
  it('keeps the Project immutable and edits only its Milestone', async () => {
    const onProjectChange = vi.fn()
    const onMilestoneChange = vi.fn()
    render(
      <ProjectControl
        project={{ id: 'p1', number: 12, name: 'Launch' }}
        milestone={null}
        onProjectChange={onProjectChange}
        onMilestoneChange={onMilestoneChange}
      />,
    )
    expect(screen.getByText('#12 Launch')).toHaveAttribute('title', '任务创建后不能移到其他项目')
    expect(screen.queryByLabelText('项目')).not.toBeInTheDocument()
    expect(onProjectChange).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByRole('option', { name: 'API ready' })).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('里程碑'), { target: { value: 'm1' } })
    expect(onMilestoneChange).toHaveBeenCalledWith({ id: 'm1', name: 'API ready' })
  })
})
