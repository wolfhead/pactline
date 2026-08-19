import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Bot, CheckCircle2, Send, UserRound } from 'lucide-react'
import {
  createThreadMessage,
  deleteThreadMessage,
  listTaskThreads,
  listThreadItems,
  updateThreadMessage,
} from '@/api/task-workflow'
import { listProjectMembers } from '@/api/projects'
import MarkdownComposer from '@/components/markdown/MarkdownComposer'
import MarkdownContent from '@/components/markdown/MarkdownContent'
import { useIdentity } from '@/identity'
import type { TaskThread, TaskThreadItem } from '@/task-types'

const KIND_LABELS: Partial<Record<TaskThreadItem['kind'], string>> = {
  progress: '进展',
  handoff: '交接',
  work_submission: '工作提交',
  review_outcome: '验收结论',
  resolution_request: '请求解决',
  resolution: '解决进展',
  issue_resolution: 'Issue 结论',
  system_event: '系统事件',
}

export default function TaskThreads({
  taskNumber,
  projectNumber,
  taskVersion,
}: {
  taskNumber: number
  projectNumber: number
  taskVersion: number
}) {
  const { me } = useIdentity()
  const [threads, setThreads] = useState<TaskThread[]>([])
  const [selectedID, setSelectedID] = useState('')
  const [items, setItems] = useState<TaskThreadItem[]>([])
  const [memberNames, setMemberNames] = useState<Map<string, string>>(new Map())
  const [body, setBody] = useState('')
  const [composerKind, setComposerKind] = useState<'message' | 'progress'>('message')
  const [editingID, setEditingID] = useState('')
  const [editingBody, setEditingBody] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    Promise.all([listTaskThreads(taskNumber), listProjectMembers(projectNumber)])
      .then(([nextThreads, members]) => {
        if (cancelled) return
        setThreads(nextThreads)
        setMemberNames(new Map(members.map(({ user }) => [user.id, user.name])))
        const main = nextThreads.find((thread) => thread.role === 'main')
        setSelectedID((current) => (
          nextThreads.some(({ id }) => id === current) ? current : (main?.id ?? nextThreads[0]?.id ?? '')
        ))
      })
      .catch((reason) => {
        if (!cancelled) setError(`加载 Thread 失败：${(reason as Error).message}`)
      })
    return () => { cancelled = true }
  }, [taskNumber, projectNumber, taskVersion])

  useEffect(() => {
    if (!selectedID) {
      setItems([])
      return
    }
    let cancelled = false
    listThreadItems(selectedID)
      .then((nextItems) => { if (!cancelled) setItems(nextItems) })
      .catch((reason) => {
        if (!cancelled) setError(`加载 Thread 内容失败：${(reason as Error).message}`)
      })
    return () => { cancelled = true }
  }, [selectedID])

  const selected = useMemo(
    () => threads.find((thread) => thread.id === selectedID) ?? null,
    [threads, selectedID],
  )
  const canPost = selected?.role === 'main' || selected?.issue_status === 'open'

  function actorName(item: TaskThreadItem) {
    if (item.author.type === 'agent') return item.author.ref || 'Agent'
    if (item.author.type === 'system') return '系统'
    return item.author.user_id === me?.id
      ? '你'
      : memberNames.get(item.author.user_id ?? '') || '成员'
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!selected || !body.trim() || busy) return
    const submittedBody = body
    setBusy(true)
    setError('')
    try {
      const kind = selected.role === 'main' ? composerKind : 'message'
      const created = await createThreadMessage(selected.id, submittedBody.trim(), kind)
      setItems((current) => [...current, created])
      setBody((current) => current === submittedBody ? '' : current)
    } catch (reason) {
      setError(`发送失败：${(reason as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  async function saveEdit(item: TaskThreadItem) {
    if (!editingBody.trim() || busy) return
    setBusy(true)
    setError('')
    try {
      const updated = await updateThreadMessage(item, editingBody.trim())
      setItems((current) => current.map((entry) => entry.id === item.id ? updated : entry))
      setEditingID('')
    } catch (reason) {
      setError(`保存失败：${(reason as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  async function remove(item: TaskThreadItem) {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      const updated = await deleteThreadMessage(item)
      setItems((current) => current.map((entry) => entry.id === item.id ? updated : entry))
    } catch (reason) {
      setError(`删除失败：${(reason as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  return (
    <section role="region" aria-label="任务 Thread" className="flex flex-col gap-3">
      <div>
        <h3 className="text-sm font-medium text-fg">Thread</h3>
        <p className="mt-0.5 text-xs text-fg-muted">
          Main Thread 保留完整上下文；阻塞问题在有明确类型的 Issue Thread 中解决，结论自动合回主线。
        </p>
      </div>

      <div className="flex gap-1 overflow-x-auto border-b border-border" role="tablist" aria-label="Thread 列表">
        {threads.map((thread, index) => {
          const label = thread.role === 'main'
            ? 'Main'
            : `${thread.issue_status === 'open' ? '待解决' : '已解决'} · ${thread.issue_type === 'decision_required' ? '决策' : '依赖'} ${index}`
          return (
            <button
              key={thread.id}
              type="button"
              role="tab"
              aria-selected={thread.id === selectedID}
              onClick={() => setSelectedID(thread.id)}
              className={thread.id === selectedID
                ? 'border-b-2 border-accent px-3 py-2 text-xs font-medium text-accent'
                : 'px-3 py-2 text-xs text-fg-muted hover:text-fg'}
            >
              {label}
            </button>
          )
        })}
      </div>

      <div className="grid gap-3">
        {items.length === 0 && (
          <p className="py-3 text-sm text-fg-muted">这个 Thread 还没有内容。</p>
        )}
        {items.map((item) => {
          const ownedMessage = item.kind === 'message'
            && item.author.type === 'user'
            && item.author.user_id === me?.id
            && !item.deleted_at
          const Icon = item.author.type === 'agent' ? Bot : item.author.type === 'system' ? CheckCircle2 : UserRound
          return (
            <article key={item.id} className="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-2">
              <span className="flex size-7 items-center justify-center rounded-full bg-surface-subtle text-fg-muted">
                <Icon className="size-3.5" aria-hidden="true" />
              </span>
              <div className="min-w-0">
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                  <span className="text-xs font-medium text-fg">{actorName(item)}</span>
                  {KIND_LABELS[item.kind] && <span className="text-[11px] text-accent">{KIND_LABELS[item.kind]}</span>}
                  <time className="text-[11px] text-fg-subtle">{new Date(item.created_at).toLocaleString()}</time>
                </div>
                {editingID === item.id ? (
                  <div className="mt-1 grid gap-2">
                    <MarkdownComposer
                      value={editingBody}
                      onChange={setEditingBody}
                      ariaLabel="编辑消息"
                      rows={3}
                    />
                    <div className="flex gap-2">
                      <button type="button" disabled={busy} onClick={() => void saveEdit(item)} className="rounded-md bg-accent px-2.5 py-1 text-xs font-medium text-white">保存</button>
                      <button type="button" onClick={() => setEditingID('')} className="px-2.5 py-1 text-xs text-fg-muted">取消</button>
                    </div>
                  </div>
                ) : item.deleted_at ? (
                  <p className="mt-1 text-sm italic text-fg-subtle">消息已删除</p>
                ) : item.issue_resolution ? (
                  <div className="mt-1 border-l border-secondary pl-3 text-sm text-fg">
                    <div className="grid grid-cols-[2.5rem_minmax(0,1fr)] gap-x-2">
                      <span className="text-fg-muted">问题</span>
                      <MarkdownContent source={item.issue_resolution.request} />
                    </div>
                    <div className="mt-2 grid grid-cols-[2.5rem_minmax(0,1fr)] gap-x-2">
                      <span className="text-fg-muted">结论</span>
                      <MarkdownContent source={item.issue_resolution.resolution} />
                    </div>
                    {threads.some(({ id }) => id === item.issue_resolution?.issue_thread_id) && (
                      <button
                        type="button"
                        onClick={() => setSelectedID(item.issue_resolution!.issue_thread_id)}
                        className="mt-1 text-xs font-medium text-secondary hover:underline"
                      >
                        查看完整 Issue
                      </button>
                    )}
                  </div>
                ) : (
                  <div className="mt-1">
                    <MarkdownContent source={item.body ?? ''} />
                  </div>
                )}
                {ownedMessage && editingID !== item.id && (
                  <div className="mt-1 flex gap-2">
                    <button type="button" onClick={() => { setEditingID(item.id); setEditingBody(item.body ?? '') }} className="text-xs text-fg-muted hover:text-fg">编辑</button>
                    <button type="button" disabled={busy} onClick={() => void remove(item)} className="text-xs text-fg-muted hover:text-danger">删除</button>
                  </div>
                )}
              </div>
            </article>
          )
        })}
      </div>

      {canPost ? (
        <form onSubmit={(event) => void submit(event)} className="flex flex-wrap items-end gap-2 border-t border-border pt-3">
          {selected?.role === 'main' && (
            <label className="grid shrink-0 gap-1 text-xs text-fg-muted">
              类型
              <select
                aria-label="Thread Item 类型"
                value={composerKind}
                onChange={(event) => setComposerKind(event.target.value as 'message' | 'progress')}
                className="rounded-md border border-border-strong bg-surface px-2 py-2 text-sm text-fg"
              >
                <option value="message">讨论</option>
                <option value="progress">进展</option>
              </select>
            </label>
          )}
          <div className="min-w-0 basis-64 flex-1">
            <MarkdownComposer
              value={body}
              onChange={setBody}
              ariaLabel="向当前 Thread 发送消息"
              placeholder="补充上下文、进展或讨论…"
              rows={3}
              disabled={busy}
            />
          </div>
          <button type="submit" disabled={!body.trim() || busy} aria-label="发送消息" className="flex size-9 shrink-0 items-center justify-center rounded-md bg-accent text-white disabled:opacity-50">
            <Send className="size-4" aria-hidden="true" />
          </button>
        </form>
      ) : selected && (
        <p className="border-t border-border pt-3 text-xs text-fg-muted">Issue 已解决，内容保持不可变；结论已合并到 Main Thread。</p>
      )}
      {error && <p role="alert" className="text-sm text-danger">{error}</p>}
    </section>
  )
}
