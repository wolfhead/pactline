import { useEffect, useRef, useState, type ChangeEvent } from 'react'
import { Download, ExternalLink, File, FileImage, FileText, Paperclip, Trash2, Upload } from 'lucide-react'
import MarkdownContent from '@/components/markdown/MarkdownContent'
import {
  completeTaskAttachmentUpload,
  createTaskAttachmentUpload,
  deleteTaskAttachment,
  listTaskAttachments,
  uploadTaskAttachment,
} from '@/api/tasks'
import type { TaskAttachment } from '@/task-types'

const MAX_ATTACHMENT_BYTES = 100 * 1024 * 1024
const MAX_MARKDOWN_PREVIEW_BYTES = 5 * 1024 * 1024

interface AttachmentSectionProps {
  taskNumber: number
  taskVersion: number
  readOnly: boolean
  onTaskChanged: () => Promise<void>
}

export default function AttachmentSection({
  taskNumber,
  taskVersion,
  readOnly,
  onTaskChanged,
}: AttachmentSectionProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [attachments, setAttachments] = useState<TaskAttachment[]>([])
  const [selected, setSelected] = useState<TaskAttachment | null>(null)
  const [markdown, setMarkdown] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    setSelected(null)
    setMarkdown('')
    listTaskAttachments(taskNumber)
      .then((items) => { if (!cancelled) setAttachments(items) })
      .catch((reason) => { if (!cancelled) setError((reason as Error).message) })
    return () => { cancelled = true }
  }, [taskNumber])

  async function choose(attachment: TaskAttachment) {
    if (attachment.preview_kind === 'html') {
      window.open(`/tasks/${taskNumber}/attachments/${attachment.id}/preview`, '_blank', 'noopener,noreferrer')
      return
    }
    if (attachment.preview_kind === 'download') {
      window.location.assign(attachment.download_url)
      return
    }
    setSelected(attachment)
    setMarkdown('')
    if (attachment.preview_kind === 'markdown') {
      if (attachment.size_bytes > MAX_MARKDOWN_PREVIEW_BYTES) {
        setError('此 Markdown 超过 5 MiB，请下载后查看。')
        return
      }
      try {
        const response = await fetch(attachment.content_url, { credentials: 'same-origin' })
        if (!response.ok) throw new Error(`读取附件失败（${response.status}）`)
        setMarkdown(await response.text())
      } catch (reason) {
        setError((reason as Error).message)
      }
    }
  }

  async function upload(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file || busy) return
    if (file.size === 0 || file.size > MAX_ATTACHMENT_BYTES) {
      setError('附件必须大于 0 字节且不超过 100 MiB。')
      return
    }
    setBusy(true)
    setError('')
    try {
      const session = await createTaskAttachmentUpload(taskNumber, file)
      await uploadTaskAttachment(session, file)
      const attachment = await completeTaskAttachmentUpload(taskNumber, session.id, taskVersion)
      setAttachments((current) => [...current, attachment])
      await onTaskChanged()
    } catch (reason) {
      setError(`上传失败：${(reason as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  async function remove(attachment: TaskAttachment) {
    setError('')
    try {
      await deleteTaskAttachment(taskNumber, attachment.id, attachment.version)
      setAttachments((current) => current.filter(({ id }) => id !== attachment.id))
      if (selected?.id === attachment.id) setSelected(null)
    } catch (reason) {
      setError(`删除失败：${(reason as Error).message}`)
    }
  }

  return (
    <section role="region" aria-label="任务附件" className="flex flex-col gap-3 border-t border-border pt-5">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="flex items-center gap-2 text-sm font-medium text-fg">
            <Paperclip className="size-4 text-fg-muted" aria-hidden="true" />
            附件
            {attachments.length > 0 && <span className="text-xs font-normal text-fg-muted">{attachments.length}</span>}
          </h3>
          <p className="mt-0.5 text-xs text-fg-muted">图片与 Markdown 可直接查看，HTML 在隔离页面打开。</p>
        </div>
        {!readOnly && (
          <>
            <input ref={inputRef} className="sr-only" type="file" onChange={(event) => void upload(event)} />
            <button
              type="button"
              disabled={busy || attachments.length >= 100}
              onClick={() => inputRef.current?.click()}
              className="inline-flex items-center gap-1.5 rounded-md border border-border-strong bg-surface px-3 py-1.5 text-xs font-medium text-fg hover:bg-surface-subtle disabled:opacity-50"
            >
              <Upload className="size-3.5" aria-hidden="true" />
              {busy ? '上传中…' : '添加附件'}
            </button>
          </>
        )}
      </div>

      {attachments.length === 0 ? (
        <p className="text-sm text-fg-muted">还没有附件。</p>
      ) : (
        <ul className="divide-y divide-border border-y border-border">
          {attachments.map((attachment) => (
            <li key={attachment.id} className="flex items-center gap-3 py-2.5">
              <AttachmentIcon attachment={attachment} />
              <button type="button" className="min-w-0 flex-1 text-left" onClick={() => void choose(attachment)}>
                <span className="block truncate text-sm font-medium text-fg hover:text-accent">{attachment.filename}</span>
                <span className="text-xs text-fg-muted">{formatBytes(attachment.size_bytes)}</span>
              </button>
              {attachment.preview_kind === 'html' && <ExternalLink className="size-3.5 text-fg-muted" aria-hidden="true" />}
              <a
                href={attachment.download_url}
                className="rounded p-1.5 text-fg-muted hover:bg-surface-subtle hover:text-fg"
                aria-label={`下载 ${attachment.filename}`}
              >
                <Download className="size-4" />
              </a>
              {!readOnly && (
                <button
                  type="button"
                  onClick={() => void remove(attachment)}
                  className="rounded p-1.5 text-fg-muted hover:bg-danger/10 hover:text-danger"
                  aria-label={`删除 ${attachment.filename}`}
                >
                  <Trash2 className="size-4" />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      {selected?.preview_kind === 'image' && (
        <div className="overflow-hidden rounded-lg border border-border bg-surface-subtle p-2">
          <img src={selected.content_url} alt={selected.filename} className="mx-auto max-h-[32rem] max-w-full object-contain" />
        </div>
      )}
      {selected?.preview_kind === 'markdown' && (
        <article className="rounded-lg border border-border bg-surface px-5 py-4">
          <MarkdownContent source={markdown} variant="document" />
        </article>
      )}
      {error && <p className="text-sm text-danger">{error}</p>}
    </section>
  )
}

function AttachmentIcon({ attachment }: { attachment: TaskAttachment }) {
  const className = 'size-4 text-fg-muted'
  if (attachment.preview_kind === 'image') return <FileImage className={className} aria-hidden="true" />
  if (attachment.preview_kind === 'markdown') return <FileText className={className} aria-hidden="true" />
  return <File className={className} aria-hidden="true" />
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}
