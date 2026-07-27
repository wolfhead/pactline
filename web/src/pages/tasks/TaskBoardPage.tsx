import { useEffect, useMemo, useState, type DragEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import PillSelect from '../../components/tasks/PillSelect'
import { listTasks, updateTask } from '../../api/tasks'
import { useIdentity } from '../../identity'
import { isTypingTarget } from '../../keyboard'
import { PRIORITY_LABELS, STATUS_LABELS, TASK_STATUSES, type Task, type TaskStatus } from '../../task-types'

const BOARD_LIMIT = 200

/**
 * A column per status; dragging a card between columns changes its status.
 * Drag-and-drop uses only the native HTML5 drag events (dragstart on the
 * card, dragover/drop on the column) — no library. Every card also carries
 * a status <select>, so the exact same move is reachable by keyboard: focus
 * the card (j/k or the arrow keys) then Tab into its status control, or use
 * the same control with a mouse instead of dragging.
 */
export default function TaskBoardPage() {
  const { me } = useIdentity()
  const navigate = useNavigate()
  const [tasks, setTasks] = useState<Task[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [reloadToken, setReloadToken] = useState(0)
  const [rowErrors, setRowErrors] = useState<Record<number, string>>({})
  const [dragOverStatus, setDragOverStatus] = useState<TaskStatus | null>(null)
  const [focusedNumber, setFocusedNumber] = useState<number | null>(null)

  // Fetches on mount, on reload, and whenever identity changes — mirrors
  // the cancelled-flag idiom in identity.tsx / WorkFeed.tsx / Board.tsx.
  useEffect(() => {
    setLoading(true)
    setError('')
    let cancelled = false
    listTasks({ limit: BOARD_LIMIT, sort: 'number', order: 'asc' })
      .then((res) => {
        if (cancelled) return
        setTasks(res.items)
      })
      .catch((err) => {
        if (cancelled) return
        setError(String((err as Error).message))
      })
      .finally(() => {
        if (cancelled) return
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [me?.id, reloadToken])

  const columns = useMemo(() => {
    const map = new Map<TaskStatus, Task[]>(TASK_STATUSES.map((s) => [s, [] as Task[]]))
    for (const t of tasks) map.get(t.status)?.push(t)
    return map
  }, [tasks])

  const flatOrder = useMemo(() => TASK_STATUSES.flatMap((s) => columns.get(s) ?? []), [columns])

  // Same optimistic-then-reconcile pattern as the list page: the card jumps
  // to its new column immediately; a server refusal snaps it back and says
  // why.
  function moveStatus(task: Task, status: TaskStatus) {
    if (task.status === status) return
    const previousStatus = task.status
    setTasks((ts) => ts.map((t) => (t.number === task.number ? { ...t, status } : t)))
    setRowErrors((e) => ({ ...e, [task.number]: '' }))
    updateTask(task.number, { status })
      .then((updated) => setTasks((ts) => ts.map((t) => (t.number === task.number ? updated : t))))
      .catch((err) => {
        setTasks((ts) => ts.map((t) => (t.number === task.number ? { ...t, status: previousStatus } : t)))
        setRowErrors((e) => ({ ...e, [task.number]: `移动失败：${(err as Error).message}，已恢复原状态` }))
      })
  }

  function handleDrop(e: DragEvent<HTMLDivElement>, status: TaskStatus) {
    e.preventDefault()
    setDragOverStatus(null)
    const number = Number(e.dataTransfer.getData('text/plain'))
    const task = tasks.find((t) => t.number === number)
    if (task) moveStatus(task, status)
  }

  // "c" jumps to the list's capture row — the board has no create
  // affordance of its own. j/k/arrows move the focused card across the
  // whole board (columns in status order), Enter opens it, Escape clears
  // the selection.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'c' && !isTypingTarget(e.target)) {
        e.preventDefault()
        navigate('/tasks', { state: { focusCreate: true } })
        return
      }
      if (isTypingTarget(e.target)) return
      if (flatOrder.length === 0) return

      const currentIndex = flatOrder.findIndex((t) => t.number === focusedNumber)
      if (e.key === 'j' || e.key === 'ArrowRight' || e.key === 'ArrowDown') {
        e.preventDefault()
        const next = flatOrder[Math.min(currentIndex + 1, flatOrder.length - 1)] ?? flatOrder[0]
        setFocusedNumber(next.number)
      } else if (e.key === 'k' || e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
        e.preventDefault()
        const prevIndex = currentIndex <= 0 ? 0 : currentIndex - 1
        setFocusedNumber(flatOrder[prevIndex]?.number ?? null)
      } else if (e.key === 'Enter' && focusedNumber != null) {
        navigate(`/tasks/${focusedNumber}`)
      } else if (e.key === 'Escape') {
        setFocusedNumber(null)
        ;(document.activeElement as HTMLElement | null)?.blur()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [flatOrder, focusedNumber, navigate])

  if (loading) return <p className="hint">正在加载看板…</p>
  if (error) {
    return (
      <p className="error">
        加载失败：{error}{' '}
        <button type="button" onClick={() => setReloadToken((t) => t + 1)}>重试</button>
      </p>
    )
  }
  if (tasks.length === 0) return <p className="hint">没有任务 — 前往列表按 C 创建一个吧</p>

  return (
    <section>
      <h2>看板</h2>
      <div className="board task-board">
        {TASK_STATUSES.map((status) => (
          <div
            key={status}
            className={`board-column ${dragOverStatus === status ? 'drag-over' : ''}`}
            onDragOver={(e) => {
              e.preventDefault()
              if (dragOverStatus !== status) setDragOverStatus(status)
            }}
            onDragLeave={() => setDragOverStatus((s) => (s === status ? null : s))}
            onDrop={(e) => handleDrop(e, status)}
          >
            <h3>
              {STATUS_LABELS[status]} <span className="hint">{(columns.get(status) ?? []).length}</span>
            </h3>
            {(columns.get(status) ?? []).map((task) => (
              <article
                key={task.id}
                className={`card task-card ${focusedNumber === task.number ? 'focused' : ''}`}
                draggable
                onDragStart={(e) => {
                  e.dataTransfer.setData('text/plain', String(task.number))
                  e.dataTransfer.effectAllowed = 'move'
                }}
              >
                <h3>
                  <Link to={`/tasks/${task.number}`}>#{task.number} {task.title}</Link>
                </h3>
                <div className="meta">
                  <span className="tag">{PRIORITY_LABELS[task.priority]}</span>
                  {task.assignee && <span>{task.assignee.name}</span>}
                  {task.due_date && <span>{task.due_date}</span>}
                </div>
                <PillSelect
                  value={task.status}
                  options={TASK_STATUSES}
                  labels={STATUS_LABELS}
                  onChange={(s) => moveStatus(task, s)}
                  ariaLabel={`任务 #${task.number} 状态（无需拖拽即可移动）`}
                />
                {rowErrors[task.number] && <span className="error">{rowErrors[task.number]}</span>}
              </article>
            ))}
          </div>
        ))}
      </div>
    </section>
  )
}
