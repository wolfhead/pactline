import { Check, UserRound } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { UserRef } from '@/task-types'

export interface UserPickerOption extends UserRef {
  description?: string
  avatarURL?: string | null
}

interface UserPickerProps {
  id: string
  options: UserPickerOption[]
  query: string
  activeOptionID: string
  selectedOptionIDs: string[]
  onActiveOptionChange: (id: string) => void
  onSelect: (option: UserPickerOption) => void
  ariaLabel?: string
  emptyLabel?: string
  selectedLabel?: string
}

export function filterUserPickerOptions(
  options: UserPickerOption[],
  query: string,
): UserPickerOption[] {
  const needle = query.trim().toLocaleLowerCase()
  if (!needle) return options
  return options.filter((option) => (
    option.name.toLocaleLowerCase().includes(needle)
    || option.email?.toLocaleLowerCase().includes(needle)
  ))
}

export default function UserPicker({
  id,
  options,
  query,
  activeOptionID,
  selectedOptionIDs,
  onActiveOptionChange,
  onSelect,
  ariaLabel = '选择用户',
  emptyLabel = '没有匹配的用户',
  selectedLabel = '已选择',
}: UserPickerProps) {
  const visibleOptions = filterUserPickerOptions(options, query)

  return (
    <div id={id} role="listbox" aria-label={ariaLabel} className="max-h-64 overflow-y-auto p-1">
      {visibleOptions.length === 0 ? (
        <p role="status" className="px-3 py-6 text-center text-sm text-fg-muted">
          {emptyLabel}
        </p>
      ) : visibleOptions.map((option) => {
        const selected = selectedOptionIDs.includes(option.id)
        const active = activeOptionID === option.id
        const accessibleName = [
          option.name,
          option.email,
          option.description,
          selected ? selectedLabel : undefined,
        ].filter(Boolean).join(' · ')
        return (
          <button
            key={option.id}
            id={`${id}-option-${option.id}`}
            type="button"
            role="option"
            aria-label={accessibleName}
            aria-selected={active}
            aria-disabled={selected}
            onMouseEnter={() => !selected && onActiveOptionChange(option.id)}
            onMouseDown={(event) => {
              event.preventDefault()
              if (!selected) onSelect(option)
            }}
            className={cn(
              'flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left outline-hidden',
              active && !selected ? 'bg-accent/10' : 'hover:bg-surface-subtle',
              selected && 'cursor-default opacity-60',
            )}
          >
            {option.avatarURL ? (
              <img
                src={option.avatarURL}
                alt=""
                className="size-7 shrink-0 rounded-full object-cover"
              />
            ) : (
              <span
                aria-hidden="true"
                className="flex size-7 shrink-0 items-center justify-center rounded-full bg-surface-subtle text-xs font-medium text-fg-muted"
              >
                {initials(option.name) || <UserRound className="size-3.5" />}
              </span>
            )}
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium text-fg">{option.name}</span>
              {(option.description || option.email) && (
                <span className="block truncate text-xs text-fg-muted">
                  {[option.description, option.email].filter(Boolean).join(' · ')}
                </span>
              )}
            </span>
            {selected && <Check aria-hidden="true" className="size-4 shrink-0 text-accent" />}
          </button>
        )
      })}
    </div>
  )
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length > 1) return `${parts[0][0]}${parts[parts.length - 1][0]}`.toUpperCase()
  return [...(parts[0] ?? '')].slice(0, 2).join('').toUpperCase()
}
