import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import ActivityLog from '../../components/tasks/ActivityLog'
import CommentSection from '../../components/tasks/CommentSection'
import InlineEditable from '../../components/tasks/InlineEditable'
import { PriorityMark, StatusMark } from '../../components/tasks/marks'
import QuietDateField from '../../components/tasks/QuietDateField'
import QuietSelect from '../../components/tasks/QuietSelect'
import { archiveTask, getTask, listLabels, restoreTask, updateTask } from '../../api/tasks'
import { useIdentity } from '../../identity'
import { isTypingTarget } from '../../keyboard'
import {
  PRIORITY_LABELS,
  STATUS_LABELS,
  TASK_PRIORITIES,
  TASK_STATUSES,
  type Label,
  type Task,
  type TaskPatchBody,
} from '../../task-types'

const UNDO_WINDOW_MS = 6000

/**
 * Title and description are edited exactly where they are read (see
 * InlineEditable). Every property commits the instant it changes —
 * optimistic update, then reconciled against whatever the server actually
 * persisted, reverting visibly with a reason if it refuses. Archiving does
 * not ask first; it offers an undo instead.
 */
export default function TaskDetailPage() {
  const { number: numberParam } = useParams<{ number: string }>()
  const number = Number(numberParam)
  const { me, users } = useIdentity()
  const navigate = useNavigate()

  const [task, setTask] = useState<Task | null>(null)
  const [error, setError] = useState('')
  const [fieldError, setFieldError] = useState('')
  const [allLabels, setAllLabels] = useState<Label[]>([])
  const [reloadToken, setReloadToken] = useState(0)
  const [undoMessage, setUndoMessage] = useState<string | null>(null)
  const undoTimerRef = useRef<number | null>(null)
  const undoActionRef = useRef<(() => void) | null>(null)

  // Fetches whenever the route number changes, a reload is requested, or
  // identity changes — mirrors the cancelled-flag idiom in identity.tsx /
  // WorkFeed.tsx / Board.tsx / BountyDetail.tsx.
  useEffect(() => {
    if (!Number.isFinite(number)) return
    setTask(null)
    setError('')
    let cancelled = false
    getTask(number)
      .then((loaded) => {
        if (cancelled) return
        setTask(loaded)
      })
      .catch((err) => {
        if (cancelled) return
        setError(String((err as Error).message))
      })
    return () => {
      cancelled = true
    }
  }, [number, me?.id, reloadToken])

  useEffect(() => {
    let cancelled = false
    listLabels()
      .then((loaded) => {
        if (cancelled) return
        setAllLabels(loaded)
      })
      .catch(() => {
        // Non-fatal: the label picker just stays empty; the task itself
        // still loaded and is fully usable.
      })
    return () => {
      cancelled = true
    }
  }, [me?.id])

  useEffect(() => {
    return () => {
      if (undoTimerRef.current) window.clearTimeout(undoTimerRef.current)
    }
  }, [])

  // Matches the shortcut legend's "Esc 关闭弹层 / 取消编辑": if an in-place
  // edit field (title/description here, or a comment body in
  // CommentSection — every one of them an InlineEditable) currently owns
  // focus, its own keydown handler reverts the draft and blurs it first;
  // `e.target` stays that same field for the rest of this event's bubble
  // (event.target never changes mid-dispatch, even once the element loses
  // focus), so isTypingTarget(e.target) is still true here and this handler
  // steps aside for that keystroke. Only once nothing is being edited does
  // Escape take the second, obvious meaning: leave the page, the same way
  // "← 返回列表" does. Never traps focus — this never calls preventDefault.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== 'Escape') return
      if (isTypingTarget(e.target)) return
      navigate('/tasks')
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [navigate])

  function patchOptimistic(patch: TaskPatchBody, optimistic: Partial<Task>) {
    if (!task) return
    const current = task
    const previous: Partial<Task> = {}
    for (const key of Object.keys(optimistic) as (keyof Task)[]) {
      previous[key] = current[key] as never
    }
    setTask({ ...current, ...optimistic })
    setFieldError('')
    updateTask(current.number, patch)
      .then((updated) => setTask(updated))
      .catch((err) => {
        setTask((t) => (t ? { ...t, ...previous } : t))
        setFieldError(`更新失败：${(err as Error).message}，已恢复原状态`)
      })
  }

  function showUndo(text: string, action: () => void) {
    if (undoTimerRef.current) window.clearTimeout(undoTimerRef.current)
    undoActionRef.current = action
    setUndoMessage(text)
    undoTimerRef.current = window.setTimeout(() => {
      setUndoMessage(null)
      undoActionRef.current = null
    }, UNDO_WINDOW_MS)
  }

  function handleArchive() {
    if (!task) return
    const number = task.number
    archiveTask(number)
      .then((updated) => {
        setTask(updated)
        showUndo('已归档任务。', () => {
          restoreTask(number)
            .then(setTask)
            .catch((err) => setFieldError(`撤销失败：${(err as Error).message}`))
        })
      })
      .catch((err) => setFieldError(`归档失败：${(err as Error).message}`))
  }

  function handleRestore() {
    if (!task) return
    restoreTask(task.number)
      .then(setTask)
      .catch((err) => setFieldError(`恢复失败：${(err as Error).message}`))
  }

  function toggleLabel(label: Label) {
    if (!task) return
    const has = task.labels.some((l) => l.ID === label.ID)
    const nextIds = has ? task.labels.filter((l) => l.ID !== label.ID).map((l) => l.ID) : [...task.labels.map((l) => l.ID), label.ID]
    const nextLabels = has ? task.labels.filter((l) => l.ID !== label.ID) : [...task.labels, label]
    patchOptimistic({ label_ids: nextIds }, { labels: nextLabels })
  }

  if (error) {
    return (
      <p className="error">
        加载失败：{error}{' '}
        <button type="button" onClick={() => setReloadToken((t) => t + 1)}>重试</button>
      </p>
    )
  }
  if (!task) return <p className="hint">正在加载任务…</p>

  return (
    <section className="task-detail">
      <p><Link to="/tasks">← 返回列表</Link></p>

      <div className="task-detail-header">
        <span className="hint">#{task.number}</span>
        <InlineEditable
          value={task.title}
          onCommit={(next) => patchOptimistic({ title: next }, { title: next })}
          ariaLabel="任务标题"
          className="task-title-input"
        />
      </div>

      <InlineEditable
        value={task.description}
        onCommit={(next) => patchOptimistic({ description: next }, { description: next })}
        multiline
        placeholder="添加描述…"
        ariaLabel="任务描述"
        className="task-description-input"
      />

      {task.archived_at && <p className="hint">此任务已归档。</p>}

      <div className="property-list">
        <div className="property-row">
          <span className="property-label">状态</span>
          <QuietSelect
            value={task.status}
            options={TASK_STATUSES}
            labels={STATUS_LABELS}
            ariaLabel="状态"
            onChange={(status) => patchOptimistic({ status }, { status })}
            renderQuiet={(status) => <StatusMark status={status} />}
          />
        </div>
        <div className="property-row">
          <span className="property-label">优先级</span>
          <QuietSelect
            value={task.priority}
            options={TASK_PRIORITIES}
            labels={PRIORITY_LABELS}
            ariaLabel="优先级"
            onChange={(priority) => patchOptimistic({ priority }, { priority })}
            renderQuiet={(priority, label) => <PriorityMark priority={priority} label={label} />}
          />
        </div>
        <div className="property-row">
          <span className="property-label">负责人</span>
          <QuietSelect
            value={task.assignee?.id ?? ''}
            options={['', ...users.map((u) => u.id)]}
            labels={Object.fromEntries([['', '未分配'], ...users.map((u) => [u.id, u.name])])}
            ariaLabel="负责人"
            onChange={(id) => {
              const assignee = id ? users.find((u) => u.id === id) ?? null : null
              patchOptimistic({ assignee_id: id || null }, { assignee })
            }}
            renderQuiet={(id) => (id ? users.find((u) => u.id === id)?.name ?? '' : '')}
          />
        </div>
        <div className="property-row">
          <span className="property-label">截止日期</span>
          <QuietDateField
            value={task.due_date}
            ariaLabel="截止日期"
            onChange={(due) => patchOptimistic({ due_date: due }, { due_date: due })}
          />
        </div>
      </div>

      <div className="label-picker" role="group" aria-label="标签">
        {allLabels.map((l) => {
          const active = task.labels.some((tl) => tl.ID === l.ID)
          return (
            <button
              key={l.ID}
              type="button"
              className={`tag filter-chip ${active ? 'active' : ''}`}
              aria-pressed={active}
              onClick={() => toggleLabel(l)}
            >
              {l.Name}
            </button>
          )
        })}
      </div>

      {fieldError && <p className="error">{fieldError}</p>}

      <div className="row">
        {task.archived_at ? (
          <button type="button" onClick={handleRestore}>恢复</button>
        ) : (
          <button type="button" onClick={handleArchive}>归档</button>
        )}
      </div>

      {undoMessage && (
        <div className="undo-toast" role="status">
          <span>{undoMessage}</span>
          <button
            type="button"
            onClick={() => {
              undoActionRef.current?.()
              if (undoTimerRef.current) window.clearTimeout(undoTimerRef.current)
              setUndoMessage(null)
            }}
          >
            撤销
          </button>
        </div>
      )}

      <p className="hint">
        创建者：{task.creator.name} · 创建于 {new Date(task.created_at).toLocaleString()}
      </p>

      <CommentSection taskNumber={task.number} />
      <ActivityLog task={task} />
    </section>
  )
}
