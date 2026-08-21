import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Bot, CheckCircle2, RefreshCw, Send, UserRound } from 'lucide-react'
import { listActivity } from '@/api/tasks'
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
import type { Activity, TaskThread, TaskThreadItem } from '@/task-types'
import { buildTaskTimeline, type TaskTimelineItem } from './task-timeline'

function reasonMessage(reason: unknown): string {
  return reason instanceof Error ? reason.message : String(reason)
}

export default function TaskThreads({
  taskNumber,
  projectNumber,
  taskVersion,
  refreshKey = 0,
}: {
  taskNumber: number
  projectNumber: number
  taskVersion: number
  refreshKey?: number
}) {
  const { me, users } = useIdentity()
  const [threads, setThreads] = useState<TaskThread[]>([])
  const [selectedID, setSelectedID] = useState('')
  const [items, setItems] = useState<TaskThreadItem[]>([])
  const [activity, setActivity] = useState<Activity[]>([])
  const [memberNames, setMemberNames] = useState<Map<string, string>>(new Map())
  const [body, setBody] = useState('')
  const [composerKind, setComposerKind] = useState<'message' | 'progress'>('message')
  const [editingID, setEditingID] = useState('')
  const [editingBody, setEditingBody] = useState('')
  const [replyToID, setReplyToID] = useState('')
  const [busy, setBusy] = useState(false)
  const [threadLoading, setThreadLoading] = useState(true)
  const [activityLoading, setActivityLoading] = useState(true)
  const [threadError, setThreadError] = useState('')
  const [itemError, setItemError] = useState('')
  const [activityError, setActivityError] = useState('')
  const [actionError, setActionError] = useState('')
  const [threadReload, setThreadReload] = useState(0)
  const [itemReload, setItemReload] = useState(0)
  const [activityReload, setActivityReload] = useState(0)

  useEffect(() => {
    setThreads([])
    setSelectedID('')
    setItems([])
    setActivity([])
    setThreadError('')
    setItemError('')
    setActivityError('')
    setActionError('')
    setReplyToID('')
  }, [taskNumber])

  useEffect(() => {
    let cancelled = false
    listProjectMembers(projectNumber)
      .then((members) => {
        if (!cancelled) setMemberNames(new Map(members.map(({ user }) => [user.id, user.name])))
      })
      .catch(() => {
        if (!cancelled) setMemberNames(new Map())
      })
    return () => { cancelled = true }
  }, [projectNumber])

  useEffect(() => {
    let cancelled = false
    setThreadLoading(true)
    setThreadError('')
    listTaskThreads(taskNumber)
      .then((nextThreads) => {
        if (cancelled) return
        setThreads(nextThreads)
        const main = nextThreads.find((thread) => thread.role === 'main')
        setSelectedID((current) => (
          nextThreads.some(({ id }) => id === current) ? current : (main?.id ?? nextThreads[0]?.id ?? '')
        ))
      })
      .catch((reason) => {
        if (cancelled) return
        setThreads([])
        setSelectedID('')
        setThreadError(`Thread 列表加载失败：${reasonMessage(reason)}`)
      })
      .finally(() => {
        if (!cancelled) setThreadLoading(false)
      })
    return () => { cancelled = true }
  }, [refreshKey, taskNumber, taskVersion, threadReload])

  useEffect(() => {
    let cancelled = false
    setActivityLoading(true)
    setActivityError('')
    listActivity(taskNumber)
      .then((nextActivity) => {
        if (!cancelled) setActivity(nextActivity)
      })
      .catch((reason) => {
        if (cancelled) return
        setActivity([])
        setActivityError(`任务变更加载失败：${reasonMessage(reason)}`)
      })
      .finally(() => {
        if (!cancelled) setActivityLoading(false)
      })
    return () => { cancelled = true }
  }, [activityReload, refreshKey, taskNumber, taskVersion])

  useEffect(() => {
    if (!selectedID) {
      setItems([])
      setItemError('')
      return
    }
    let cancelled = false
    setItems([])
    setItemError('')
    setReplyToID('')
    listThreadItems(selectedID)
      .then((nextItems) => {
        if (!cancelled) setItems(nextItems)
      })
      .catch((reason) => {
        if (cancelled) return
        setItems([])
        setItemError(`Thread 内容加载失败：${reasonMessage(reason)}`)
      })
    return () => { cancelled = true }
  }, [itemReload, refreshKey, selectedID, taskVersion])

  const selected = useMemo(
    () => threads.find((thread) => thread.id === selectedID) ?? null,
    [threads, selectedID],
  )
  const canPost = selected?.role === 'main' || selected?.issue_status === 'open'
  const userNameById = useMemo(() => {
    const names = Object.fromEntries(users.map((user) => [user.id, user.name]))
    for (const [id, name] of memberNames) names[id] = name
    return names
  }, [memberNames, users])
  const showActivity = selected?.role === 'main' || (!selected && threads.length === 0)
  const timeline = useMemo(() => buildTaskTimeline({
    threadItems: items,
    activity: showActivity ? activity : [],
    currentUserID: me?.id,
    userNameById,
  }), [activity, items, me?.id, showActivity, userNameById])

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!selected || !body.trim() || busy) return
    const submittedBody = body
    setBusy(true)
    setActionError('')
    try {
      const kind = replyToID ? 'message' : selected.role === 'main' ? composerKind : 'message'
      const created = replyToID
        ? await createThreadMessage(selected.id, submittedBody.trim(), kind, [], replyToID)
        : await createThreadMessage(selected.id, submittedBody.trim(), kind)
      setItems((current) => [...current, created])
      setBody((current) => current === submittedBody ? '' : current)
      setReplyToID('')
    } catch (reason) {
      setActionError(`发送失败：${reasonMessage(reason)}`)
    } finally {
      setBusy(false)
    }
  }

  async function saveEdit(item: TaskThreadItem) {
    if (!editingBody.trim() || busy) return
    setBusy(true)
    setActionError('')
    try {
      const updated = await updateThreadMessage(item, editingBody.trim())
      setItems((current) => current.map((entry) => entry.id === item.id ? updated : entry))
      setEditingID('')
    } catch (reason) {
      setActionError(`保存失败：${reasonMessage(reason)}`)
    } finally {
      setBusy(false)
    }
  }

  async function remove(item: TaskThreadItem) {
    if (busy) return
    setBusy(true)
    setActionError('')
    try {
      const updated = await deleteThreadMessage(item)
      setItems((current) => current.map((entry) => entry.id === item.id ? updated : entry))
    } catch (reason) {
      setActionError(`删除失败：${reasonMessage(reason)}`)
    } finally {
      setBusy(false)
    }
  }

  function renderTimelineBody(timelineItem: TaskTimelineItem) {
    const item = timelineItem.threadItem
    if (!item) return <p className="mt-1 text-sm leading-6 text-fg">{timelineItem.body}</p>
    if (editingID === item.id) {
      return (
        <div className="mt-2 grid gap-2">
          <MarkdownComposer value={editingBody} onChange={setEditingBody} ariaLabel="编辑消息" rows={3} />
          <div className="flex gap-2">
            <button type="button" disabled={busy} onClick={() => void saveEdit(item)} className="rounded-md bg-accent px-2.5 py-1 text-xs font-medium text-white disabled:opacity-50">保存</button>
            <button type="button" onClick={() => setEditingID('')} className="px-2.5 py-1 text-xs text-fg-muted hover:text-fg">取消</button>
          </div>
        </div>
      )
    }
    if (item.deleted_at) return <p className="mt-1 text-sm italic text-fg-subtle">消息已删除</p>
    if (item.issue_resolution) {
      return (
        <div className="mt-2 border-l border-secondary pl-3 text-sm text-fg">
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
      )
    }
    return <div className="mt-1"><MarkdownContent source={item.body ?? ''} /></div>
  }

  const replyTarget = items.find((item) => item.id === replyToID)

  return (
    <section role="region" aria-label="工作时间线" className="flex min-w-0 flex-col gap-4">
      <div>
        <h3 className="text-sm font-semibold text-fg">工作时间线</h3>
        <p className="mt-0.5 text-xs leading-5 text-fg-muted">
          讨论、执行、交付与任务变更按发生时间汇入 Main；Issue 保留独立讨论与只读结论。
        </p>
      </div>

      {(threads.length > 0 || threadLoading) && (
        <div className="flex gap-1 overflow-x-auto border-b border-border" role="tablist" aria-label="Thread 列表">
          {threads.map((thread, index) => {
            const issueNumber = threads.slice(0, index + 1).filter(({ role }) => role === 'issue').length
            const label = thread.role === 'main'
              ? 'Main'
              : `${thread.issue_status === 'open' ? '待解决' : '已解决'} · ${thread.issue_type === 'decision_required' ? '决策' : '依赖'} ${issueNumber}`
            return (
              <button
                key={thread.id}
                type="button"
                role="tab"
                aria-selected={thread.id === selectedID}
                onClick={() => setSelectedID(thread.id)}
                className={thread.id === selectedID
                  ? 'border-b-2 border-accent px-3 py-2 text-xs font-medium text-accent'
                  : 'border-b-2 border-transparent px-3 py-2 text-xs text-fg-muted hover:text-fg'}
              >
                {label}
              </button>
            )
          })}
          {threadLoading && threads.length === 0 && <span className="px-3 py-2 text-xs text-fg-muted">正在加载 Thread…</span>}
        </div>
      )}

      {threadError && (
        <div role="alert" className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-danger-subtle px-3 py-2 text-sm text-danger">
          <span>{threadError}；任务变更仍可查看。</span>
          <button type="button" onClick={() => setThreadReload((value) => value + 1)} className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium hover:bg-surface">
            <RefreshCw className="size-3.5" aria-hidden="true" />重试 Thread
          </button>
        </div>
      )}
      {itemError && (
        <div role="alert" className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-danger-subtle px-3 py-2 text-sm text-danger">
          <span>{itemError}；任务变更仍可查看。</span>
          <button type="button" onClick={() => setItemReload((value) => value + 1)} className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium hover:bg-surface">
            <RefreshCw className="size-3.5" aria-hidden="true" />重试内容
          </button>
        </div>
      )}
      {activityError && showActivity && (
        <div role="alert" className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-danger-subtle px-3 py-2 text-sm text-danger">
          <span>{activityError}；Thread 讨论仍可查看。</span>
          <button type="button" onClick={() => setActivityReload((value) => value + 1)} className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium hover:bg-surface">
            <RefreshCw className="size-3.5" aria-hidden="true" />重试任务变更
          </button>
        </div>
      )}

      {timeline.length === 0 && !threadLoading && !activityLoading && !threadError && !itemError && !activityError && (
        <p className="py-3 text-sm text-fg-muted">这条时间线还没有内容。</p>
      )}
      {timeline.length > 0 && (
        <ol className="relative grid gap-3 before:absolute before:bottom-3 before:left-[0.8125rem] before:top-3 before:w-px before:bg-border">
          {timeline.map((timelineItem) => {
            const item = timelineItem.threadItem
            const ownedMessage = item?.kind === 'message'
              && item.author.type === 'user'
              && item.author.user_id === me?.id
              && !item.deleted_at
            const Icon = timelineItem.actor.type === 'agent'
              ? Bot
              : timelineItem.actor.type === 'system' ? CheckCircle2 : UserRound
            const compact = timelineItem.kind === 'system_event' || timelineItem.kind === 'task_change'
            return (
              <li key={timelineItem.id}>
                <article
                  data-timeline-kind={timelineItem.kind}
                  className={`relative grid grid-cols-[1.75rem_minmax(0,1fr)] gap-2 ${compact ? 'py-0.5' : ''}`}
                >
                  <span className={`z-10 flex size-7 items-center justify-center rounded-full ${
                    timelineItem.actor.type === 'agent'
                      ? 'bg-accent-subtle text-accent'
                      : timelineItem.actor.type === 'system'
                        ? 'bg-secondary-subtle text-secondary'
                        : 'bg-surface-subtle text-fg-muted'
                  }`}>
                    <Icon className="size-3.5" aria-hidden="true" />
                  </span>
                  <div className={`min-w-0 ${compact ? 'rounded-md bg-surface-subtle px-3 py-2' : 'pt-0.5'}`}>
                    <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                      <span className="text-xs font-medium text-fg">{timelineItem.actor.label}</span>
                      <span className={`text-[11px] ${timelineItem.kind === 'task_change' ? 'text-fg-muted' : 'text-accent'}`}>
                        {timelineItem.kindLabel}
                      </span>
                      <time dateTime={timelineItem.occurredAt} className="text-[11px] text-fg-subtle">
                        {new Date(timelineItem.occurredAt).toLocaleString()}
                      </time>
                    </div>
                    {renderTimelineBody(timelineItem)}
                    {item?.reply_to_item_id && (
                      <p className="mt-1 text-xs text-fg-muted">
                        回复 {items.find((candidate) => candidate.id === item.reply_to_item_id)?.body?.slice(0, 60) || '较早的消息'}
                      </p>
                    )}
                    {ownedMessage && editingID !== item.id && (
                      <div className="mt-1 flex gap-2">
                        <button type="button" onClick={() => { setEditingID(item.id); setEditingBody(item.body ?? '') }} className="text-xs text-fg-muted hover:text-fg">编辑</button>
                        <button type="button" disabled={busy} onClick={() => void remove(item)} className="text-xs text-fg-muted hover:text-danger">删除</button>
                      </div>
                    )}
                    {item && item.kind !== 'system_event' && !item.deleted_at && canPost && (
                      <button
                        type="button"
                        onClick={() => { setReplyToID(item.id); setComposerKind('message') }}
                        className="mt-1 text-xs text-fg-muted hover:text-accent"
                      >
                        回复
                      </button>
                    )}
                  </div>
                </article>
              </li>
            )
          })}
        </ol>
      )}

      {canPost ? (
        <form onSubmit={(event) => void submit(event)} className="flex flex-wrap items-end gap-2 border-t border-border pt-3">
          {replyTarget && (
            <div className="flex basis-full items-center justify-between gap-2 rounded-md bg-accent-subtle px-3 py-2 text-xs text-fg">
              <span className="min-w-0 truncate">正在回复：{replyTarget.body || '这条消息'}</span>
              <button type="button" onClick={() => setReplyToID('')} className="shrink-0 font-medium text-accent hover:underline">取消回复</button>
            </div>
          )}
          {selected?.role === 'main' && (
            <label className="grid shrink-0 gap-1 text-xs text-fg-muted">
              类型
              <select
                aria-label="Thread Item 类型"
                value={composerKind}
                onChange={(event) => setComposerKind(event.target.value as 'message' | 'progress')}
                className="rounded-md border border-border-strong bg-surface px-2 py-2 text-sm text-fg focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/20"
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
      {actionError && <p role="alert" className="text-sm text-danger">{actionError}</p>}
    </section>
  )
}
