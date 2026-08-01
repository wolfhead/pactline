import { useEffect, useState } from 'react'
import { ArrowLeft, Download } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { listTaskAttachments } from '@/api/tasks'
import type { TaskAttachment } from '@/task-types'

export default function AttachmentPreviewPage() {
  const params = useParams<{ number: string; attachmentID: string }>()
  const taskNumber = Number(params.number)
  const [attachment, setAttachment] = useState<TaskAttachment | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    listTaskAttachments(taskNumber)
      .then((items) => {
        if (cancelled) return
        const found = items.find(({ id }) => id === params.attachmentID)
        if (!found) setError('附件不存在或已被删除。')
        else setAttachment(found)
      })
      .catch((reason) => { if (!cancelled) setError((reason as Error).message) })
    return () => { cancelled = true }
  }, [taskNumber, params.attachmentID])

  return (
    <main className="flex h-dvh flex-col bg-surface">
      <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-4">
        <Link to={`/tasks/${taskNumber}`} className="rounded p-1.5 text-fg-muted hover:bg-surface-subtle hover:text-fg" aria-label="返回任务">
          <ArrowLeft className="size-4" />
        </Link>
        <h1 className="min-w-0 flex-1 truncate text-sm font-medium text-fg">{attachment?.filename ?? '附件预览'}</h1>
        {attachment && (
          <a href={attachment.download_url} className="inline-flex items-center gap-1.5 text-xs font-medium text-fg-muted hover:text-fg">
            <Download className="size-4" /> 下载
          </a>
        )}
      </header>
      {attachment ? (
        <iframe
          title={attachment.filename}
          src={attachment.content_url}
          sandbox="allow-scripts"
          referrerPolicy="no-referrer"
          className="min-h-0 flex-1 border-0 bg-white"
        />
      ) : (
        <div className="grid flex-1 place-items-center p-6 text-sm text-fg-muted">{error || '正在载入附件…'}</div>
      )}
    </main>
  )
}
