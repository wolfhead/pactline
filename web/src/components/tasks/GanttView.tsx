import { useMemo, useRef, useState } from 'react'
import { AlertCircle, ArrowRight, CornerDownRight, Link2, Undo2 } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { Tier } from '@/hooks/useBreakpoint'
import { cn } from '@/lib/utils'
import type { Task } from '@/task-types'
import { orderTasksWithChildren } from './task-hierarchy'
import type { TaskCollectionController } from './useTaskCollection'

const DAY_WIDTH = 28
const ROW_HEIGHT = 48
const HEADER_HEIGHT = 48

function parseDate(value: string): Date {
  return new Date(`${value}T12:00:00Z`)
}

function formatDate(value: Date): string {
  return value.toISOString().slice(0, 10)
}

function addDays(value: Date, days: number): Date {
  const next = new Date(value)
  next.setUTCDate(next.getUTCDate() + days)
  return next
}

function dayDifference(left: Date, right: Date): number {
  return Math.round((left.getTime() - right.getTime()) / 86_400_000)
}

function taskRange(task: Task): { start: string; end: string } | null {
  const start = task.start_date ?? task.due_date
  const end = task.due_date ?? task.start_date
  return start && end ? { start, end } : null
}

function summaryRange(task: Task, tasks: Task[]): { start: string; end: string } | null {
  const members = [task, ...tasks.filter((candidate) => candidate.parent?.number === task.number)]
  const ranges = members.map(taskRange).filter((range): range is { start: string; end: string } => Boolean(range))
  if (!ranges.length) return null
  return {
    start: ranges.map((range) => range.start).sort()[0],
    end: ranges.map((range) => range.end).sort().at(-1)!,
  }
}

interface DragState {
  task: Task
  mode: 'move' | 'start' | 'end'
  originX: number
  days: number
}

export default function GanttView({
  controller,
  tier,
  selectedNumber,
}: {
  controller: TaskCollectionController
  tier: Tier
  selectedNumber: number | null
}) {
  const tasks = useMemo(() => orderTasksWithChildren(controller.tasks), [controller.tasks])
  const labelWidth = tier === 'phone' ? 196 : tier === 'md' ? 240 : 288
  const [drag, setDrag] = useState<DragState | null>(null)
  const [undo, setUndo] = useState<{ number: number; days: number } | null>(null)
  const undoTimer = useRef<number>()
  const today = useMemo(() => parseDate(new Date().toISOString().slice(0, 10)), [])

  const timeline = useMemo(() => {
    const dates = controller.tasks.flatMap((task) => (
      [task.start_date, task.due_date].filter((value): value is string => Boolean(value))
    ))
    const earliest = dates.length ? parseDate([...dates].sort()[0]) : addDays(today, -7)
    const latest = dates.length ? parseDate([...dates].sort().at(-1)!) : addDays(today, 28)
    const start = addDays(earliest < today ? earliest : today, -5)
    const end = addDays(latest > today ? latest : today, 10)
    const days = Array.from(
      { length: dayDifference(end, start) + 1 },
      (_, index) => addDays(start, index),
    )
    return { start, end, days, width: days.length * DAY_WIDTH }
  }, [controller.tasks, today])

  const positions = useMemo(() => {
    const byNumber = new Map<number, { row: number; start: number; end: number }>()
    tasks.forEach((task, row) => {
      const range = task.children?.length
        ? summaryRange(task, controller.tasks)
        : taskRange(task)
      if (!range) return
      byNumber.set(task.number, {
        row,
        start: dayDifference(parseDate(range.start), timeline.start) * DAY_WIDTH,
        end: (dayDifference(parseDate(range.end), timeline.start) + 1) * DAY_WIDTH,
      })
    })
    return byNumber
  }, [controller.tasks, tasks, timeline.start])

  function beginDrag(
    event: React.PointerEvent,
    task: Task,
    mode: DragState['mode'],
  ) {
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    setDrag({ task, mode, originX: event.clientX, days: 0 })
  }

  function continueDrag(event: React.PointerEvent) {
    if (!drag) return
    setDrag({
      ...drag,
      days: Math.round((event.clientX - drag.originX) / DAY_WIDTH),
    })
  }

  function resizeSchedule(task: Task, mode: 'start' | 'end', days: number) {
    const range = taskRange(task)
    if (!range) return
    if (mode === 'start') {
      const nextStart = addDays(parseDate(range.start), days)
      if (nextStart > parseDate(range.end)) return
      const start = formatDate(nextStart)
      controller.patchOptimistic(
        task,
        { start_date: start },
        { start_date: start },
      )
      return
    }
    const nextEnd = addDays(parseDate(range.end), days)
    if (nextEnd < parseDate(range.start)) return
    const end = formatDate(nextEnd)
    controller.patchOptimistic(
      task,
      { due_date: end },
      { due_date: end },
    )
  }

  function endDrag(event: React.PointerEvent) {
    if (!drag) return
    event.currentTarget.releasePointerCapture(event.pointerId)
    const current = drag
    setDrag(null)
    if (current.days === 0) return
    if (current.mode === 'move') {
      void controller.shiftSchedule(current.task, current.days).then((updated) => {
        if (!updated) return
        if (undoTimer.current) window.clearTimeout(undoTimer.current)
        setUndo({ number: current.task.number, days: current.days })
        undoTimer.current = window.setTimeout(() => setUndo(null), 6000)
      })
      return
    }
    resizeSchedule(current.task, current.mode, current.days)
  }

  function scheduleAtPointer(event: React.MouseEvent, task: Task) {
    if (taskRange(task)) return
    const bounds = event.currentTarget.getBoundingClientRect()
    const day = Math.max(
      0,
      Math.min(timeline.days.length - 1, Math.floor((event.clientX - bounds.left) / DAY_WIDTH)),
    )
    const value = formatDate(timeline.days[day])
    controller.patchOptimistic(
      task,
      { start_date: value, due_date: value },
      { start_date: value, due_date: value },
    )
  }

  const dependencyPaths = tasks.flatMap((task) => {
    const target = positions.get(task.number)
    if (!target) return []
    return (task.dependencies ?? []).flatMap((dependency) => {
      const source = positions.get(dependency.number)
      if (!source) return []
      const x1 = source.end
      const y1 = HEADER_HEIGHT + source.row * ROW_HEIGHT + ROW_HEIGHT / 2
      const x2 = target.start
      const y2 = HEADER_HEIGHT + target.row * ROW_HEIGHT + ROW_HEIGHT / 2
      const bend = Math.max(x1 + 12, x2 - 16)
      return [{
        key: `${dependency.number}-${task.number}`,
        path: `M ${x1} ${y1} H ${bend} V ${y2} H ${x2}`,
      }]
    })
  })

  return (
    <div className="relative min-h-0 overflow-auto bg-surface" data-gantt-view>
      {undo && (
        <div
          role="status"
          className="sticky left-3 top-2 z-40 mb-[-40px] flex w-fit items-center gap-3 rounded-lg bg-fg px-3 py-2 text-sm text-white shadow-[0_8px_24px_rgb(23_43_61/0.18)]"
        >
          排期已移动 {Math.abs(undo.days)} 天
          <button
            type="button"
            className="flex items-center gap-1 font-medium text-white underline underline-offset-4"
            onClick={() => {
              const task = controller.tasks.find((candidate) => candidate.number === undo.number)
              if (task) void controller.shiftSchedule(task, -undo.days)
              setUndo(null)
            }}
          >
            <Undo2 className="size-3.5" aria-hidden="true" />
            撤销
          </button>
        </div>
      )}
      <div
        className="relative"
        style={{
          minWidth: labelWidth + timeline.width,
          height: HEADER_HEIGHT + tasks.length * ROW_HEIGHT,
        }}
      >
        <div
          className="sticky left-0 top-0 z-30 flex items-center border-b border-r border-border bg-surface px-3 text-xs font-medium text-fg-muted"
          style={{ width: labelWidth, height: HEADER_HEIGHT }}
        >
          任务
          <span className="ml-auto font-normal text-fg-subtle">拖动排期 · 拉伸边缘</span>
        </div>
        <div
          className="absolute top-0 flex border-b border-border bg-surface-subtle"
          style={{ left: labelWidth, width: timeline.width, height: HEADER_HEIGHT }}
        >
          {timeline.days.map((day) => {
            const isToday = formatDate(day) === formatDate(today)
            return (
              <div
                key={formatDate(day)}
                className={cn(
                  'flex shrink-0 flex-col items-center justify-center border-r border-border/70 text-[10px] text-fg-muted',
                  isToday && 'bg-accent-subtle font-semibold text-accent',
                )}
                style={{ width: DAY_WIDTH }}
              >
                <span>{day.toLocaleDateString('zh-CN', { weekday: 'narrow', timeZone: 'UTC' })}</span>
                <span>{day.getUTCDate()}</span>
              </div>
            )
          })}
        </div>

        <svg
          aria-hidden="true"
          className="pointer-events-none absolute z-20 overflow-visible"
          style={{
            left: labelWidth,
            top: 0,
            width: timeline.width,
            height: HEADER_HEIGHT + tasks.length * ROW_HEIGHT,
          }}
        >
          <defs>
            <marker id="gantt-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse">
              <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--color-secondary)" />
            </marker>
          </defs>
          {dependencyPaths.map((item) => (
            <path
              key={item.key}
              d={item.path}
              fill="none"
              stroke="var(--color-secondary)"
              strokeWidth="1.25"
              strokeOpacity="0.7"
              markerEnd="url(#gantt-arrow)"
            />
          ))}
        </svg>

        {tasks.map((task, row) => {
          const range = task.children?.length
            ? summaryRange(task, controller.tasks)
            : taskRange(task)
          const isSummary = Boolean(task.children?.length)
          const previewDays = drag?.task.number === task.number ? drag.days : 0
          const left = range
            ? dayDifference(parseDate(range.start), timeline.start) * DAY_WIDTH
            : 0
          const width = range
            ? Math.max(DAY_WIDTH, (dayDifference(parseDate(range.end), parseDate(range.start)) + 1) * DAY_WIDTH)
            : 0
          const dueOnly = Boolean(!task.start_date && task.due_date && !isSummary)
          return (
            <div key={task.id}>
              <div
                className={cn(
                  'sticky left-0 z-10 flex items-center gap-2 border-b border-r border-border bg-surface px-3',
                  task.number === selectedNumber &&
                    'bg-accent-subtle shadow-[inset_3px_0_0_var(--color-accent)]',
                )}
                style={{
                  width: labelWidth,
                  height: ROW_HEIGHT,
                }}
              >
                {task.parent ? (
                  <CornerDownRight className="ml-3 size-3.5 shrink-0 text-fg-subtle" aria-hidden="true" />
                ) : (
                  <span className="w-3.5 shrink-0" />
                )}
                <Link
                  to={`/tasks/${task.number}`}
                  className="min-w-0 flex-1 truncate text-sm text-fg hover:text-accent focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent/30"
                  title={task.title}
                >
                  {task.title}
                </Link>
                {controller.rowErrors[task.number] && (
                  <span
                    role="alert"
                    className="flex shrink-0 items-center gap-1 text-[11px] text-danger"
                    title={controller.rowErrors[task.number]}
                  >
                    <AlertCircle className="size-3" aria-hidden="true" />
                    更新失败
                  </span>
                )}
                {task.blocked && (
                  <span
                    className="flex shrink-0 items-center gap-1 text-[11px] text-status-in-progress"
                    title="等待依赖任务完成"
                  >
                    <Link2 className="size-3" aria-hidden="true" />
                    阻塞
                  </span>
                )}
              </div>
              <div
                className="absolute border-b border-border bg-[repeating-linear-gradient(to_right,transparent_0,transparent_27px,var(--color-border)_28px)]"
                style={{
                  left: labelWidth,
                  top: HEADER_HEIGHT + row * ROW_HEIGHT,
                  width: timeline.width,
                  height: ROW_HEIGHT,
                }}
                title={range ? undefined : '点击日期安排为一天任务'}
              >
                {!range && (
                  <button
                    type="button"
                    className="absolute inset-0 z-10 flex items-center gap-1 px-2 text-left text-xs text-fg-subtle hover:bg-accent-subtle/40 hover:text-fg"
                    onClick={(event) => scheduleAtPointer(event, task)}
                    aria-label={`为任务 ${task.title} 安排日期`}
                  >
                    <ArrowRight className="size-3" aria-hidden="true" />
                    点击日期安排
                  </button>
                )}
                {range && (
                  <div
                    className={cn(
                      'absolute top-0 h-12 touch-none overflow-visible',
                      isSummary
                        ? 'rounded-md'
                        : dueOnly
                          ? 'rounded-md'
                          : 'rounded-md',
                    )}
                    style={{
                      left: dueOnly ? left - (44 - DAY_WIDTH) / 2 : left,
                      width: dueOnly ? 44 : width,
                      transform: `translateX(${previewDays * DAY_WIDTH}px)`,
                    }}
                  >
                    <button
                      type="button"
                      aria-label={`${task.title}，${range.start} 至 ${range.end}`}
                      className={cn(
                        'gantt-bar-action absolute flex items-center justify-center overflow-hidden text-xs font-medium focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent/40',
                        isSummary
                          ? 'rounded-md bg-fg text-white shadow-[0_4px_12px_rgb(23_43_61/0.16)]'
                          : dueOnly
                            ? 'rounded-md text-status-in-progress'
                            : 'rounded-md bg-accent text-white shadow-[0_4px_12px_rgb(23_43_61/0.16)]',
                      )}
                      onPointerDown={(event) => beginDrag(event, task, 'move')}
                      onPointerMove={continueDrag}
                      onPointerUp={endDrag}
                      onKeyDown={(event) => {
                        if (event.key === 'ArrowLeft') void controller.shiftSchedule(task, -1)
                        if (event.key === 'ArrowRight') void controller.shiftSchedule(task, 1)
                      }}
                    >
                      {dueOnly ? (
                        <span
                          className="size-4 rotate-45 rounded-sm bg-status-in-progress shadow-[0_4px_12px_rgb(23_43_61/0.16)]"
                          aria-hidden="true"
                        />
                      ) : (
                        <span className="pointer-events-none truncate px-2">
                          {isSummary ? `${task.title} · ${task.children.length + 1} 项` : task.title}
                        </span>
                      )}
                    </button>
                    {!isSummary && !dueOnly && (
                      <>
                        <button
                          type="button"
                          aria-label={`调整 ${task.title} 开始日期`}
                          className="gantt-resize-handle absolute bottom-2 left-0 top-2 w-6 cursor-ew-resize rounded-l-md bg-white/25 opacity-0 hover:opacity-100 focus-visible:opacity-100"
                          onPointerDown={(event) => {
                            event.stopPropagation()
                            beginDrag(event, task, 'start')
                          }}
                          onPointerMove={continueDrag}
                          onPointerUp={endDrag}
                          onKeyDown={(event) => {
                            if (event.key === 'ArrowLeft') resizeSchedule(task, 'start', -1)
                            if (event.key === 'ArrowRight') resizeSchedule(task, 'start', 1)
                          }}
                        />
                        <button
                          type="button"
                          aria-label={`调整 ${task.title} 截止日期`}
                          className="gantt-resize-handle absolute bottom-2 right-0 top-2 w-6 cursor-ew-resize rounded-r-md bg-white/25 opacity-0 hover:opacity-100 focus-visible:opacity-100"
                          onPointerDown={(event) => {
                            event.stopPropagation()
                            beginDrag(event, task, 'end')
                          }}
                          onPointerMove={continueDrag}
                          onPointerUp={endDrag}
                          onKeyDown={(event) => {
                            if (event.key === 'ArrowLeft') resizeSchedule(task, 'end', -1)
                            if (event.key === 'ArrowRight') resizeSchedule(task, 'end', 1)
                          }}
                        />
                      </>
                    )}
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
