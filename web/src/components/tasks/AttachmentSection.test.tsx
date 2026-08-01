import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import AttachmentSection from './AttachmentSection'
import * as tasksApi from '@/api/tasks'
import type { TaskAttachment, TaskAttachmentUpload } from '@/task-types'

vi.mock('@/api/tasks')

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const ATTACHMENT: TaskAttachment = {
  id: 'attachment-1', task_id: 'task-1', uploader_id: 'u1',
  filename: 'prototype.html', media_type: 'text/html', size_bytes: 11,
  preview_kind: 'html', version: 1,
  content_url: '/content?disposition=inline', download_url: '/content?disposition=attachment',
  created_at: '', updated_at: '',
}

const UPLOAD: TaskAttachmentUpload = {
  id: 'upload-1', provider: 'local', filename: 'prototype.html',
  media_type: 'text/html', size_bytes: 11, direct: false, method: 'PUT',
  upload_url: '/upload', headers: {}, expires_at: '',
}

describe('AttachmentSection', () => {
  beforeEach(() => {
    vi.mocked(tasksApi.listTaskAttachments).mockResolvedValue([])
    vi.mocked(tasksApi.createTaskAttachmentUpload).mockResolvedValue(UPLOAD)
    vi.mocked(tasksApi.uploadTaskAttachment).mockResolvedValue()
    vi.mocked(tasksApi.completeTaskAttachmentUpload).mockResolvedValue(ATTACHMENT)
  })

  it('uploads through a session and exposes the safe preview action', async () => {
    const onTaskChanged = vi.fn().mockResolvedValue(undefined)
    const { container } = render(
      <AttachmentSection
        taskNumber={42}
        taskVersion={3}
        readOnly={false}
        onTaskChanged={onTaskChanged}
      />,
    )
    await screen.findByText('还没有附件。')
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['<h1>Ok</h1>'], 'prototype.html', { type: 'text/html' })
    fireEvent.change(input, { target: { files: [file] } })

    await screen.findByText('prototype.html')
    expect(tasksApi.createTaskAttachmentUpload).toHaveBeenCalledWith(42, file)
    expect(tasksApi.uploadTaskAttachment).toHaveBeenCalledWith(UPLOAD, file)
    expect(tasksApi.completeTaskAttachmentUpload).toHaveBeenCalledWith(42, 'upload-1', 3)
    await waitFor(() => expect(onTaskChanged).toHaveBeenCalled())
    expect(screen.getByRole('link', { name: '下载 prototype.html' }))
      .toHaveAttribute('href', ATTACHMENT.download_url)
  })
})
