import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { AlertCircle, ArrowRight, CornerDownRight, Flag, Link2, Undo2 } from 'lucide-react'
import { Link } from 'react-router-dom'
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card'
import type { Tier } from '@/hooks/useBreakpoint'
import { cn } from '@/lib/utils'
import {
  PRIORITY_LABELS,
  PHASE_LABELS,
  type Task,
  type TaskPriority,
} from '@/task-types'
import { orderTasksWithChildren } from './task-hierarchy'
import type { TaskCollectionController } from './useTaskCollection'
import type { TaskNavigationState } from './task-navigation'

const DAY_WIDTH = 36
const ROW_HEIGHT = 56
const HEADER_HEIGHT = 64
const WEEK_HEADER_HEIGHT = 28

const PRIORITY_META_CLASS: Record<TaskPriority, string> = {
  none: 'text-fg-subtle',
  low: 'text-priority-low',
  medium: 'text-priority-medium',
  high: 'text-priority-high',
  urgent: 'text-priority-urgent',
}

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

function isoWeekNumber(value: Date): number {
  const date = new Date(Date.UTC(
    value.getUTCFullYear(),
    value.getUTCMonth(),
    value.getUTCDate(),
  ))
  const weekday = date.getUTCDay() || 7
  date.setUTCDate(date.getUTCDate() + 4 - weekday)
  const yearStart = new Date(Date.UTC(date.getUTCFullYear(), 0, 1))
  return Math.ceil((dayDifference(date, yearStart) + 1) / 7)
}

function isWeekend(value: Date): boolean {
  return value.getUTCDay() === 0 || value.getUTCDay() === 6
}

function isWeekStart(value: Date): boolean {
  return value.getUTCDay() === 1
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

function scheduleLabel(task: Task): string {
  if (task.start_date && task.due_date) {
    return task.start_date === task.due_date
      ? `单日任务 · ${task.start_date}`
      : `${task.start_date} 至 ${task.due_date}`
  }
  if (task.due_date) return `仅截止日期 · ${task.due_date}`
  if (task.start_date) return `开始于 ${task.start_date}`
  return '尚未排期'
}

function rangeLabel(
  range: { start: string; end: string },
  dueOnly: boolean,
): string {
  if (dueOnly) return `仅截止日期 · ${range.end}`
  if (range.start === range.end) return `单日任务 · ${range.start}`
  return `${range.start} 至 ${range.end}`
}

function TaskPreview({
  task,
  children,
}: {
  task: Task
  children: ReactNode
}) {
  return (
    <HoverCard openDelay={500} closeDelay={120}>
      <HoverCardTrigger asChild>{children}</HoverCardTrigger>
      <HoverCardContent>
        <div className="flex items-center gap-2 text-xs text-fg-muted">
          <span className="font-mono">#{task.number}</span>
          <span>{PHASE_LABELS[task.phase]}</span>
          <span>·</span>
          <span>{PRIORITY_LABELS[task.priority]}</span>
        </div>
        <h3 className="mt-1.5 text-sm font-semibold leading-5 text-fg">{task.title}</h3>
        <dl className="mt-3 grid grid-cols-[4.5rem_minmax(0,1fr)] gap-x-2 gap-y-1.5 text-xs">
          <dt className="text-fg-muted">负责人</dt>
          <dd>{task.assignee?.name ?? '未分配'}</dd>
          <dt className="text-fg-muted">排期</dt>
          <dd>{scheduleLabel(task)}</dd>
          {task.parent && (
            <>
              <dt className="text-fg-muted">父任务</dt>
              <dd className="truncate">#{task.parent.number} {task.parent.title}</dd>
            </>
          )}
        </dl>
        {task.blocked && (
          <p className="mt-3 flex items-center gap-1.5 rounded-md bg-status-in-progress/10 px-2 py-1.5 text-xs text-status-in-progress">
            <Link2 className="size-3.5 shrink-0" aria-hidden="true" />
            等待 {task.dependencies.filter((item) => !['done', 'cancelled'].includes(item.phase)).length} 个前置任务
          </p>
        )}
        <p className="mt-3 border-t border-border pt-2 text-xs text-fg-subtle">点击打开任务详情</p>
      </HoverCardContent>
    </HoverCard>
  )
}

export default function GanttView({
  controller,
  tier,
  selectedNumber,
  taskHref = (task) => `/tasks/${task.number}`,
  taskLinkState,
}: {
  controller: TaskCollectionController
  tier: Tier
  selectedNumber: number | null
  taskHref?: (task: Task) => string
  taskLinkState?: TaskNavigationState
}) {
  const tasks = useMemo(() => orderTasksWithChildren(controller.tasks), [controller.tasks])
  const labelWidth = tier === 'phone' ? 220 : tier === 'md' ? 272 : 320
  const scrollContainer = useRef<HTMLDivElement>(null)
  const [viewportWidth, setViewportWidth] = useState(0)
  const [drag, setDrag] = useState<DragState | null>(null)
  const [undo, setUndo] = useState<{ number: number; days: number } | null>(null)
  const undoTimer = useRef<number>()
  const suppressTaskOpen = useRef(false)
  const today = useMemo(() => parseDate(new Date().toISOString().slice(0, 10)), [])

  useEffect(() => {
    const node = scrollContainer.current
    if (!node) return
    const measure = () => setViewportWidth(node.clientWidth)
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    return () => observer.disconnect()
  }, [])

  const timeline = useMemo(() => {
    const dates = controller.tasks.flatMap((task) => (
      [task.start_date, task.due_date].filter((value): value is string => Boolean(value))
    ))
    const earliest = dates.length ? parseDate([...dates].sort()[0]) : addDays(today, -7)
    const latest = dates.length ? parseDate([...dates].sort().at(-1)!) : addDays(today, 28)
    const start = addDays(earliest < today ? earliest : today, -5)
    const naturalEnd = addDays(latest > today ? latest : today, 10)
    const visibleDays = Math.max(
      1,
      Math.ceil(Math.max(0, viewportWidth - labelWidth) / DAY_WIDTH),
    )
    const minimumEnd = addDays(start, visibleDays - 1)
    const end = naturalEnd > minimumEnd ? naturalEnd : minimumEnd
    const days = Array.from(
      { length: dayDifference(end, start) + 1 },
      (_, index) => addDays(start, index),
    )
    const weeks = days.reduce<Array<{
      key: string
      label: string
      days: number
    }>>((groups, day) => {
      const weekday = day.getUTCDay() || 7
      const weekStart = addDays(day, 1 - weekday)
      const month = `${weekStart.getUTCMonth() + 1}月`
      const week = isoWeekNumber(day)
      const key = formatDate(weekStart)
      const previous = groups.at(-1)
      if (previous?.key === key) {
        previous.days += 1
      } else {
        groups.push({ key, label: `${month} · 第${week}周`, days: 1 })
      }
      return groups
    }, [])
    return { start, end, days, weeks, width: days.length * DAY_WIDTH }
  }, [controller.tasks, labelWidth, today, viewportWidth])

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
    suppressTaskOpen.current = false
    event.currentTarget.setPointerCapture(event.pointerId)
    setDrag({ task, mode, originX: event.clientX, days: 0 })
  }

  function continueDrag(event: React.PointerEvent) {
    if (!drag) return
    if (Math.abs(event.clientX - drag.originX) > 4) {
      suppressTaskOpen.current = true
    }
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
    window.setTimeout(() => {
      suppressTaskOpen.current = false
    }, 0)
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
    <div
      ref={scrollContainer}
      className="relative min-h-0 overflow-auto bg-surface"
      data-gantt-view
      data-timeline-days={timeline.days.length}
    >
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
          minWidth: Math.max(viewportWidth, labelWidth + timeline.width),
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
          className="absolute top-0 border-b border-border bg-surface-subtle"
          style={{ left: labelWidth, width: timeline.width, height: HEADER_HEIGHT }}
        >
          <div className="flex border-b border-border" style={{ height: WEEK_HEADER_HEIGHT }}>
            {timeline.weeks.map((week) => (
              <div
                key={week.key}
                className="flex shrink-0 items-center border-r border-border-strong/60 px-2 text-[10px] font-medium text-fg-muted"
                style={{ width: week.days * DAY_WIDTH }}
                title={week.label}
              >
                <span className="truncate">{week.label}</span>
              </div>
            ))}
          </div>
          <div className="flex" style={{ height: HEADER_HEIGHT - WEEK_HEADER_HEIGHT }}>
            {timeline.days.map((day) => {
              const isToday = formatDate(day) === formatDate(today)
              return (
                <div
                  key={formatDate(day)}
                  className={cn(
                    'flex shrink-0 items-center justify-center gap-1 border-r border-border/70 text-[10px] text-fg-muted',
                    isWeekStart(day) && 'border-l border-l-border-strong/60',
                    isWeekend(day) && 'bg-secondary/5',
                    isToday && 'bg-accent-subtle font-semibold text-accent',
                  )}
                  style={{ width: DAY_WIDTH }}
                  data-weekend={isWeekend(day) || undefined}
                >
                  <span>{day.toLocaleDateString('zh-CN', { weekday: 'narrow', timeZone: 'UTC' })}</span>
                  <span>{day.getUTCDate()}</span>
                </div>
              )
            })}
          </div>
        </div>

        <div
          aria-hidden="true"
          className="pointer-events-none absolute flex"
          style={{
            left: labelWidth,
            top: HEADER_HEIGHT,
            width: timeline.width,
            height: tasks.length * ROW_HEIGHT,
          }}
        >
          {timeline.days.map((day) => (
            <div
              key={formatDate(day)}
              className={cn(
                'shrink-0 border-r border-border/70',
                isWeekStart(day) && 'border-l border-l-border-strong/60',
                isWeekend(day) && 'bg-secondary/5',
                formatDate(day) === formatDate(today) && 'bg-accent-subtle/40',
              )}
              style={{ width: DAY_WIDTH }}
              data-weekend-band={isWeekend(day) || undefined}
            />
          ))}
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
                <div className="min-w-0 flex-1">
                  <TaskPreview task={task}>
                    <Link
                      to={taskHref(task)}
                      state={taskLinkState}
                      className="block truncate text-sm font-medium text-fg hover:text-accent focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent/30"
                    >
                      {task.title}
                    </Link>
                  </TaskPreview>
                  <div className="mt-0.5 flex min-w-0 items-center gap-2 text-[11px] leading-4">
                    <span className="flex min-w-0 items-center gap-1 text-fg-muted">
                      <span
                        className="grid size-4 shrink-0 place-items-center rounded-full bg-surface-subtle text-[9px] font-medium text-fg-muted"
                        aria-hidden="true"
                      >
                        {task.assignee?.name.slice(0, 1).toUpperCase() ?? '—'}
                      </span>
                      <span className="max-w-24 truncate">{task.assignee?.name ?? '未分配'}</span>
                    </span>
                    <span
                      className={cn(
                        'flex shrink-0 items-center gap-1',
                        PRIORITY_META_CLASS[task.priority],
                      )}
                    >
                      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
                      {PRIORITY_LABELS[task.priority]}
                    </span>
                  </div>
                </div>
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
                className="absolute border-b border-border"
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
                      'absolute top-1 h-12 touch-none overflow-visible',
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
                    <TaskPreview task={task}>
                      <Link
                        to={taskHref(task)}
                        state={taskLinkState}
                        draggable={false}
                        aria-label={`${task.title}，${rangeLabel(range, dueOnly)}`}
                        className={cn(
                          'gantt-bar-action absolute flex items-center justify-center overflow-hidden text-xs font-medium focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent/40',
                          isSummary
                            ? 'rounded-md bg-fg text-white shadow-[0_4px_12px_rgb(23_43_61/0.16)]'
                            : dueOnly
                              ? 'rounded-md text-status-in-progress'
                              : 'rounded-md bg-accent text-white shadow-[0_4px_12px_rgb(23_43_61/0.16)]',
                          task.number === selectedNumber && 'ring-2 ring-accent ring-offset-2 ring-offset-surface',
                        )}
                        onClick={(event) => {
                          if (suppressTaskOpen.current) event.preventDefault()
                        }}
                        onDragStart={(event) => event.preventDefault()}
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
                            className="flex h-8 w-9 items-center justify-center rounded-md border border-status-in-progress/40 bg-status-in-progress/10 shadow-[0_4px_12px_rgb(23_43_61/0.12)]"
                            aria-hidden="true"
                          >
                            <Flag className="size-4 fill-current" />
                          </span>
                        ) : width >= 108 ? (
                          <span className="pointer-events-none truncate px-2">
                            {isSummary ? `${task.title} · ${task.children.length + 1} 项` : task.title}
                          </span>
                        ) : (
                          <span className="pointer-events-none h-1.5 w-3 rounded-full bg-white/80" aria-hidden="true" />
                        )}
                      </Link>
                    </TaskPreview>
                    {!isSummary && !dueOnly && (
                      <>
                        <button
                          type="button"
                          aria-label={`调整 ${task.title} 开始日期`}
                          className="gantt-resize-handle absolute bottom-2 left-0 top-2 z-10 w-6 cursor-ew-resize rounded-l-md bg-white/25 opacity-0 hover:opacity-100 focus-visible:opacity-100"
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
                          className="gantt-resize-handle absolute bottom-2 right-0 top-2 z-10 w-6 cursor-ew-resize rounded-r-md bg-white/25 opacity-0 hover:opacity-100 focus-visible:opacity-100"
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
