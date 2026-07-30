import { useEffect, useState } from 'react'
import { CalendarDays, ChevronLeft, ChevronRight } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { CONTROL_TRIGGER_CLASS, PROPERTY_ICON_SLOT_CLASS } from './trigger'
import { cn } from '@/lib/utils'

const WEEKDAYS = ['一', '二', '三', '四', '五', '六', '日']

function parseDate(value: string | null): Date | null {
  if (!value) return null
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!match) return null
  return new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
}

function toDateValue(date: Date): string {
  const year = String(date.getFullYear())
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function sameDay(left: Date, right: Date): boolean {
  return toDateValue(left) === toDateValue(right)
}

function calendarDays(month: Date): Date[] {
  const first = new Date(month.getFullYear(), month.getMonth(), 1)
  const mondayOffset = (first.getDay() + 6) % 7
  const start = new Date(first)
  start.setDate(first.getDate() - mondayOffset)
  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(start)
    date.setDate(start.getDate() + index)
    return date
  })
}

function triggerLabel(value: string | null, emptyLabel: string): string {
  const date = parseDate(value)
  if (!date) return emptyLabel
  return `${date.getMonth() + 1}月${date.getDate()}日`
}

/** A product-styled date popover shared by list rows and task detail
 * cards. It deliberately avoids the browser-native date picker, whose
 * appearance and behavior vary by platform. */
export default function DueDateControl({
  value, onChange, ariaLabel, emptyLabel = '无截止', pickerLabel = '选择截止日期',
}: {
  value: string | null
  onChange: (next: string | null) => void
  ariaLabel: string
  emptyLabel?: string
  pickerLabel?: string
}) {
  const [open, setOpen] = useState(false)
  const selected = parseDate(value)
  const today = new Date()
  const [visibleMonth, setVisibleMonth] = useState(
    () => selected ?? new Date(today.getFullYear(), today.getMonth(), 1),
  )

  useEffect(() => {
    if (open) {
      const base = parseDate(value) ?? new Date()
      setVisibleMonth(new Date(base.getFullYear(), base.getMonth(), 1))
    }
  }, [open, value])

  function selectDate(date: Date) {
    onChange(toDateValue(date))
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        data-property-control
        aria-label={ariaLabel}
        className={cn(CONTROL_TRIGGER_CLASS, !value && 'text-fg-muted')}
      >
        <span className={PROPERTY_ICON_SLOT_CLASS} aria-hidden="true">
          <CalendarDays className="size-3.5 text-fg-muted" />
        </span>
        {triggerLabel(value, emptyLabel)}
      </PopoverTrigger>
      <PopoverContent
        align="end"
        aria-label={pickerLabel}
        className="w-[17.5rem] p-3 [@media(pointer:coarse)]:w-[21rem]"
      >
        <div className="flex items-center justify-between">
          <button
            type="button"
            onClick={() => setVisibleMonth(new Date(
              visibleMonth.getFullYear(),
              visibleMonth.getMonth() - 1,
              1,
            ))}
            aria-label="上个月"
            className="flex size-8 items-center justify-center rounded-md text-fg-muted hover:bg-surface-subtle hover:text-fg"
          >
            <ChevronLeft className="size-4" />
          </button>
          <p aria-live="polite" className="text-sm font-medium">
            {visibleMonth.getFullYear()}年{visibleMonth.getMonth() + 1}月
          </p>
          <button
            type="button"
            onClick={() => setVisibleMonth(new Date(
              visibleMonth.getFullYear(),
              visibleMonth.getMonth() + 1,
              1,
            ))}
            aria-label="下个月"
            className="flex size-8 items-center justify-center rounded-md text-fg-muted hover:bg-surface-subtle hover:text-fg"
          >
            <ChevronRight className="size-4" />
          </button>
        </div>

        <div className="mt-2 grid grid-cols-7 text-center text-[11px] text-fg-muted">
          {WEEKDAYS.map((weekday) => (
            <span key={weekday} className="py-1">{weekday}</span>
          ))}
        </div>
        <div role="grid" className="grid grid-cols-7 gap-0.5">
          {calendarDays(visibleMonth).map((date) => {
            const isSelected = selected ? sameDay(date, selected) : false
            const isToday = sameDay(date, today)
            const outsideMonth = date.getMonth() !== visibleMonth.getMonth()
            return (
              <button
                key={toDateValue(date)}
                type="button"
                role="gridcell"
                aria-label={`${date.getFullYear()}年${date.getMonth() + 1}月${date.getDate()}日`}
                aria-selected={isSelected}
                aria-current={isToday ? 'date' : undefined}
                onClick={() => selectDate(date)}
                className={cn(
                  'flex size-9 items-center justify-center rounded-md text-sm outline-none hover:bg-accent-subtle focus-visible:ring-2 focus-visible:ring-accent',
                  outsideMonth && 'text-fg-subtle',
                  isToday && !isSelected && 'font-semibold text-accent',
                  isSelected && 'bg-accent text-accent-fg hover:bg-accent',
                )}
              >
                {date.getDate()}
              </button>
            )
          })}
        </div>

        <div className="mt-2 flex items-center justify-between border-t border-border pt-2">
          <button
            type="button"
            onClick={() => selectDate(today)}
            className="rounded-md px-2 py-1.5 text-xs font-medium text-accent hover:bg-accent-subtle"
          >
            今天
          </button>
          <button
            type="button"
            disabled={!value}
            onClick={() => {
              onChange(null)
              setOpen(false)
            }}
            className="rounded-md px-2 py-1.5 text-xs text-fg-muted hover:bg-surface-subtle hover:text-fg disabled:pointer-events-none disabled:opacity-40"
          >
            清除日期
          </button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
