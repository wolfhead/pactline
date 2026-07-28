import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { UserRef } from '@/task-types'
import { CONTROL_TRIGGER_CLASS, PROPERTY_ICON_SLOT_CLASS } from './trigger'

// Radix Select treats "" as "no value" and would render the placeholder
// instead of 未分配, so unassigned needs a sentinel that is not the empty
// string. It never leaves this component: onChange maps it back to null,
// which is what TaskPatchBody.assignee_id needs in order to clear.
const UNASSIGNED = '__unassigned__'

/** Assignee as a permanently visible control. "未分配" (unassigned) is a
 * real, always-selectable option — never a blank trigger — and is reported
 * to the caller as `null`, matching TaskPatchBody.assignee_id's clear
 * semantics; `''` would be sent as a literal (invalid) user id. */
export default function AssigneeControl({
  value, users, onChange, ariaLabel, disabled,
}: {
  value: string | null
  users: UserRef[]
  onChange: (next: string | null) => void
  ariaLabel: string
  disabled?: boolean
}) {
  return (
    <Select
      value={value ?? UNASSIGNED}
      onValueChange={(v) => onChange(v === UNASSIGNED ? null : v)}
      disabled={disabled}
    >
      <SelectTrigger
        data-property-control
        aria-label={ariaLabel}
        className={CONTROL_TRIGGER_CLASS}
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={UNASSIGNED}>
          <span className="flex items-center gap-1.5">
            <span className={PROPERTY_ICON_SLOT_CLASS} aria-hidden="true" />
            <span>未分配</span>
          </span>
        </SelectItem>
        {users.map((u) => (
          <SelectItem key={u.id} value={u.id}>
            <span className="flex items-center gap-1.5">
              <span className={PROPERTY_ICON_SLOT_CLASS} aria-hidden="true" />
              <span>{u.name}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
