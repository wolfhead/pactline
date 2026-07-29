import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Checkbox } from '@/components/ui/checkbox'
import type { Label } from '@/task-types'
import { CONTROL_TRIGGER_CLASS, PROPERTY_ICON_SLOT_CLASS } from './trigger'
import { cn } from '@/lib/utils'

/** Labels as a permanently visible control. The trigger always shows the
 * current label names, joined, or 无标签 as a real, always-rendered state —
 * never a blank — without opening anything first; the popover holds a
 * checkbox per known label.
 *
 * onChange always reports the complete next set of label ids, never an
 * increment: TaskPatchBody.label_ids replaces the whole set on the wire, so
 * a caller sending only the just-toggled id would silently drop every other
 * selected label. */
export default function LabelControl({
  value, all, onChange, ariaLabel,
}: {
  value: Label[]
  all: Label[]
  onChange: (nextIds: string[]) => void
  ariaLabel: string
}) {
  function toggle(id: string) {
    const current = value.map((l) => l.id)
    const next = current.includes(id) ? current.filter((x) => x !== id) : [...current, id]
    onChange(next)
  }

  return (
    <Popover>
      <PopoverTrigger
        data-property-control
        aria-label={ariaLabel}
        className={cn(CONTROL_TRIGGER_CLASS, value.length === 0 && 'text-fg-muted')}
      >
        <span className={PROPERTY_ICON_SLOT_CLASS} aria-hidden="true" />
        <span>{value.length > 0 ? value.map((l) => l.name).join(', ') : '无标签'}</span>
      </PopoverTrigger>
      <PopoverContent className="w-56 p-2">
        {/* list-none/pl-0, matching FilterBar's near-identical popover list:
            stated rather than left to preflight, so a bare <ul> in a popover
            can never regrow the UA bullet marker and 40px indent if the reset
            ever moves. */}
        <ul className="flex list-none flex-col gap-1 pl-0">
          {all.map((label) => {
            const checked = value.some((l) => l.id === label.id)
            return (
              <li key={label.id}>
                <label className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent-subtle">
                  <Checkbox
                    aria-label={label.name}
                    checked={checked}
                    onCheckedChange={() => toggle(label.id)}
                  />
                  {label.name}
                </label>
              </li>
            )
          })}
        </ul>
      </PopoverContent>
    </Popover>
  )
}
