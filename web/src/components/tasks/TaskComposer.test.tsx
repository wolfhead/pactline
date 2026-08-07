import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TaskComposerProvider, useTaskComposer } from './TaskComposer'
import * as projectsAPI from '@/api/projects'
import * as tasksAPI from '@/api/tasks'
import type { Task, TaskAttachment, TaskAttachmentUpload } from '@/task-types'

vi.mock('@/api/projects')
vi.mock('@/api/tasks')
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    users: [{ id: 'u1', name: 'Alex', email: 'a@example.com' }],
    isReadOnly: false,
  }),
}))

const CREATED_TASK: Task = {
  id: 'task-1',
  number: 42,
  version: 1,
  title: 'Clarify the work',
  context: 'The request has no durable context.',
  expected_result: 'The task is understandable without chat history.',
  description: '',
  status: 'todo',
  priority: 'none',
  assignee: null,
  creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
  start_date: null,
  due_date: null,
  project: { id: 'project-1', number: 12, name: 'Task Manager' },
  milestone: { id: 'milestone-1', name: 'Structured creation' },
  labels: [],
  parent: null,
  children: [],
  dependencies: [],
  dependents: [],
  blocked: false,
  created_at: '',
  updated_at: '',
  completed_at: null,
  archived_at: null,
}

const UPLOAD: TaskAttachmentUpload = {
  id: 'upload-1',
  provider: 'local',
  filename: 'brief.md',
  media_type: 'text/markdown',
  size_bytes: 12,
  direct: false,
  method: 'PUT',
  upload_url: '/upload',
  headers: {},
  expires_at: '',
}

const ATTACHMENT: TaskAttachment = {
  id: 'attachment-1',
  task_id: CREATED_TASK.id,
  uploader_id: 'u1',
  filename: 'brief.md',
  media_type: 'text/markdown',
  size_bytes: 12,
  preview_kind: 'markdown',
  version: 1,
  content_url: '/content',
  download_url: '/download',
  created_at: '',
  updated_at: '',
}

function ContextualTrigger({ onCreated }: { onCreated: (task: Task) => void }) {
  const { openTaskComposer } = useTaskComposer()
  return (
    <button
      type="button"
      onClick={() => openTaskComposer({
        projectNumber: 12,
        milestoneID: 'milestone-1',
        onCreated,
      })}
    >
      Open composer
    </button>
  )
}

describe('TaskComposer', () => {
  beforeEach(() => {
    vi.mocked(projectsAPI.listProjects).mockResolvedValue([{
      id: 'project-1',
      number: 12,
      version: 1,
      name: 'Task Manager',
      description: '',
      creator: CREATED_TASK.creator,
      archived_at: null,
      created_at: '',
      updated_at: '',
      completed_tasks: 0,
      eligible_tasks: 0,
    }])
    vi.mocked(projectsAPI.getProject).mockResolvedValue({
      project: {
        id: 'project-1',
        number: 12,
        version: 1,
        name: 'Task Manager',
        description: '',
        creator: CREATED_TASK.creator,
        archived_at: null,
        created_at: '',
        updated_at: '',
        completed_tasks: 0,
        eligible_tasks: 0,
      },
      milestones: [{
        id: 'milestone-1',
        project_id: 'project-1',
        version: 1,
        name: 'Structured creation',
        outcome: 'Tasks have complete briefs',
        description: '',
        owner_id: 'u1',
        status: 'active',
        target_date: null,
        position: 0,
        completed_at: null,
        cancelled_at: null,
        created_at: '',
        updated_at: '',
        acceptance_criteria: [],
      }],
      tasks: [],
      activity: [],
    })
    vi.mocked(tasksAPI.createTask).mockResolvedValue(CREATED_TASK)
    vi.mocked(tasksAPI.createTaskAttachmentUpload).mockResolvedValue(UPLOAD)
    vi.mocked(tasksAPI.uploadTaskAttachment).mockResolvedValue()
    vi.mocked(tasksAPI.completeTaskAttachmentUploadVersioned).mockResolvedValue({
      attachment: ATTACHMENT,
      taskVersion: 2,
    })
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('requires a structured brief and preserves contextual project defaults', async () => {
    const onCreated = vi.fn()
    render(
      <MemoryRouter>
        <TaskComposerProvider>
          <ContextualTrigger onCreated={onCreated} />
        </TaskComposerProvider>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Open composer' }))

    const title = await screen.findByRole('textbox', { name: /标题/ })
    const context = screen.getByRole('textbox', { name: /背景 \/ 问题/ })
    const expectedResult = screen.getByRole('textbox', { name: /期望结果/ })
    expect(title).toBeRequired()
    expect(context).toBeRequired()
    expect(expectedResult).toBeRequired()

    await waitFor(() => expect(screen.getByRole('combobox', { name: /项目/ })).toHaveValue('12'))
    await waitFor(() => expect(screen.getByRole('combobox', { name: '里程碑' })).toHaveValue('milestone-1'))

    fireEvent.change(title, { target: { value: CREATED_TASK.title } })
    fireEvent.change(context, { target: { value: CREATED_TASK.context } })
    fireEvent.change(expectedResult, { target: { value: CREATED_TASK.expected_result } })
    fireEvent.click(screen.getByRole('button', { name: '创建任务' }))

    await waitFor(() => expect(tasksAPI.createTask).toHaveBeenCalledWith({
      title: CREATED_TASK.title,
      context: CREATED_TASK.context,
      expected_result: CREATED_TASK.expected_result,
      project_number: 12,
      milestone_id: 'milestone-1',
      assignee_id: null,
      priority: 'none',
      execution_mode: 'human_only',
    }))
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(CREATED_TASK))
  })

  it('creates once and uploads selected attachments as one submitted flow', async () => {
    const secondUpload = { ...UPLOAD, id: 'upload-2', filename: 'screen.png' }
    vi.mocked(tasksAPI.createTaskAttachmentUpload)
      .mockResolvedValueOnce(UPLOAD)
      .mockResolvedValueOnce(secondUpload)
    vi.mocked(tasksAPI.completeTaskAttachmentUploadVersioned)
      .mockResolvedValueOnce({ attachment: ATTACHMENT, taskVersion: 2 })
      .mockResolvedValueOnce({
        attachment: { ...ATTACHMENT, id: 'attachment-2', filename: 'screen.png' },
        taskVersion: 3,
      })
    const onCreated = vi.fn()
    render(
      <MemoryRouter>
        <TaskComposerProvider>
          <ContextualTrigger onCreated={onCreated} />
        </TaskComposerProvider>
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Open composer' }))
    const title = await screen.findByRole('textbox', { name: /标题/ })
    const context = screen.getByRole('textbox', { name: /背景 \/ 问题/ })
    const expectedResult = screen.getByRole('textbox', { name: /期望结果/ })
    fireEvent.change(title, { target: { value: CREATED_TASK.title } })
    fireEvent.change(context, { target: { value: CREATED_TASK.context } })
    fireEvent.change(expectedResult, { target: { value: CREATED_TASK.expected_result } })
    const file = new File(['task context'], 'brief.md', { type: 'text/markdown' })
    const secondFile = new File(['image'], 'screen.png', { type: 'image/png' })
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file, secondFile] } })

    expect(screen.getByText('brief.md')).toBeInTheDocument()
    expect(screen.getByText('screen.png')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '创建任务' }))

    await waitFor(() => expect(tasksAPI.completeTaskAttachmentUploadVersioned).toHaveBeenNthCalledWith(
      1, 42, 'upload-1', 1,
    ))
    await waitFor(() => expect(tasksAPI.completeTaskAttachmentUploadVersioned).toHaveBeenNthCalledWith(
      2, 42, 'upload-2', 2,
    ))
    expect(tasksAPI.createTaskAttachmentUpload).toHaveBeenCalledWith(42, file)
    expect(tasksAPI.createTaskAttachmentUpload).toHaveBeenCalledWith(42, secondFile)
    expect(tasksAPI.uploadTaskAttachment).toHaveBeenCalledWith(UPLOAD, file)
    expect(tasksAPI.uploadTaskAttachment).toHaveBeenCalledWith(secondUpload, secondFile)
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith({
      ...CREATED_TASK,
      version: 3,
    }))
  })

  it('keeps the created task and retries attachment upload without creating twice', async () => {
    vi.mocked(tasksAPI.createTaskAttachmentUpload)
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce(UPLOAD)
    const onCreated = vi.fn()
    render(
      <MemoryRouter>
        <TaskComposerProvider>
          <ContextualTrigger onCreated={onCreated} />
        </TaskComposerProvider>
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Open composer' }))
    const title = await screen.findByRole('textbox', { name: /标题/ })
    fireEvent.change(title, { target: { value: CREATED_TASK.title } })
    fireEvent.change(screen.getByRole('textbox', { name: /背景 \/ 问题/ }), {
      target: { value: CREATED_TASK.context },
    })
    fireEvent.change(screen.getByRole('textbox', { name: /期望结果/ }), {
      target: { value: CREATED_TASK.expected_result },
    })
    const file = new File(['task context'], 'brief.md', { type: 'text/markdown' })
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })
    fireEvent.click(screen.getByRole('button', { name: '创建任务' }))

    expect(await screen.findByText(/任务 #42 已创建，但附件上传未完成/)).toBeInTheDocument()
    expect(onCreated).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '重试上传' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith({
      ...CREATED_TASK,
      version: 2,
    }))
    expect(tasksAPI.createTask).toHaveBeenCalledTimes(1)
    expect(tasksAPI.createTaskAttachmentUpload).toHaveBeenCalledTimes(2)
  })
})
