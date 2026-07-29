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
      owner: { id: 'u1', name: 'Alex', email: 'a@example.com' },
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
    owner: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    archived_at: null, created_at: '', updated_at: '', completed_tasks: 0,
    eligible_tasks: 0,
  }
}

describe('ProjectControl', () => {
  it('selects a project and then limits milestones to that project', async () => {
    const onProjectChange = vi.fn()
    const onMilestoneChange = vi.fn()
    const { rerender } = render(
      <ProjectControl
        project={{ id: 'p1', number: 12, name: 'Launch' }}
        milestone={null}
        onProjectChange={onProjectChange}
        onMilestoneChange={onMilestoneChange}
      />,
    )
    await waitFor(() => expect(screen.getByRole('option', { name: '#12 Launch' })).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('项目'), { target: { value: '12' } })
    expect(onProjectChange).toHaveBeenCalledWith({ id: 'p1', number: 12, name: 'Launch' })

    rerender(
      <ProjectControl
        project={{ id: 'p1', number: 12, name: 'Launch' }}
        milestone={null}
        onProjectChange={onProjectChange}
        onMilestoneChange={onMilestoneChange}
      />,
    )
    await waitFor(() => expect(screen.getByRole('option', { name: 'API ready' })).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('里程碑'), { target: { value: 'm1' } })
    expect(onMilestoneChange).toHaveBeenCalledWith({ id: 'm1', name: 'API ready' })
  })
})
