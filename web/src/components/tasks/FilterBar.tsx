import { Plus } from 'lucide-react'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { useIdentity } from '@/identity'
import {
  PRIORITY_LABELS,
  STATUS_LABELS,
  TASK_PRIORITIES,
  TASK_STATUSES,
  type Label,
  type TaskPriority,
  type TaskStatus,
} from '@/task-types'
import { Checkbox } from '@/components/ui/checkbox'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { CONTROL_TRIGGER_CLASS } from './controls/trigger'
import LabelManager from './LabelManager'

export interface TaskFilters {
  statuses: TaskStatus[]
  priorities: TaskPriority[]
  assignee: string // '' = anyone, 'none' = unassigned, else a user id
  labelId: string // '' = any label
  search: string
  sort: string
  order: string
}

export const DEFAULT_FILTERS: TaskFilters = {
  statuses: [],
  priorities: [],
  assignee: '',
  labelId: '',
  search: '',
  sort: 'created_at',
  order: 'desc',
}

const SORT_OPTIONS: [string, string][] = [
  ['created_at', '创建时间'],
  ['updated_at', '更新时间'],
  ['due_date', '截止日期'],
  ['priority', '优先级'],
  ['number', '编号'],
]

// Radix Select treats "" as "no value" and would fall back to the
// placeholder instead of showing 所有人 — same reasoning as AssigneeControl's
// UNASSIGNED sentinel. Never leaves this component: onChange maps it back to
// '', which is what TaskFilters.assignee's "anyone" state needs.
const ANY_ASSIGNEE = '__any__'

// Shared shape for the two Popover-based multi-select triggers (状态/优先级),
// so both sit in the row with the same size as the five row controls
// (CONTROL_TRIGGER_CLASS) while still showing whether they're narrowing the
// list — the only cue an always-visible filter row has, since nothing here
// is hidden behind a single master disclosure anymore.
function filterTriggerClass(active: boolean) {
  return cn(
    CONTROL_TRIGGER_CLASS,
    active && 'border-accent bg-accent-subtle text-accent',
  )
}

interface FilterBarProps {
  filters: TaskFilters
  onChange: (next: TaskFilters) => void
  labels: Label[]
  onLabelsChanged: (labels: Label[]) => void
  onRequestCreate: () => void
}

/** Every filter is independent and additive — toggling one never clears
 * another, since "filters combine" is the whole point of a list view over a
 * fixed dataset. Status/priority are Popover + checkbox list (they're the
 * two multi-value filters); assignee and sort are single-value Selects;
 * label is a Popover too, since LabelManager's create/rename/delete UI folds
 * into the bottom of it rather than living behind its own top-level entry.
 *
 * There is no longer one master "筛选" disclosure hiding all of this: each
 * filter is its own permanently visible trigger, in the same register as
 * the five per-row property controls, and highlights itself
 * (`aria-pressed`/accent background) only when it's actually narrowing the
 * list. */
export default function FilterBar({ filters, onChange, labels, onLabelsChanged, onRequestCreate }: FilterBarProps) {
  const { users } = useIdentity()
  const tier = useBreakpoint()

  function toggleStatus(s: TaskStatus) {
    const has = filters.statuses.includes(s)
    onChange({ ...filters, statuses: has ? filters.statuses.filter((x) => x !== s) : [...filters.statuses, s] })
  }

  function togglePriority(p: TaskPriority) {
    const has = filters.priorities.includes(p)
    onChange({ ...filters, priorities: has ? filters.priorities.filter((x) => x !== p) : [...filters.priorities, p] })
  }

  function toggleLabel(id: string) {
    onChange({ ...filters, labelId: filters.labelId === id ? '' : id })
  }

  const statusActive = filters.statuses.length > 0
  const priorityActive = filters.priorities.length > 0
  const assigneeActive = filters.assignee !== ''
  const labelActive = filters.labelId !== ''
  const selectedLabel = labelActive ? labels.find((l) => l.ID === filters.labelId) : undefined

  return (
    <div className="mt-2 flex flex-wrap items-center gap-2">
      <input
        value={filters.search}
        onChange={(e) => onChange({ ...filters, search: e.target.value })}
        placeholder="搜索标题或描述…"
        aria-label="搜索任务"
        className="h-8 min-w-[180px] max-w-xs flex-1 rounded-md border border-border-strong bg-surface px-2.5 text-sm text-fg outline-none placeholder:text-fg-muted focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/50"
      />

      <Popover>
        <PopoverTrigger aria-pressed={statusActive} className={filterTriggerClass(statusActive)}>
          状态{statusActive && ` · ${filters.statuses.length}`}
        </PopoverTrigger>
        <PopoverContent className="w-48 p-2" role="group" aria-label="按状态筛选">
          {/* list-none/pl-0 are stated rather than left to preflight: they
              are what a bare <ul> in a popover needs either way, and spelling
              them out keeps this list from silently regrowing the UA marker
              and 40px indent if the reset ever moves (caught by screenshot
              when the old stylesheet went). */}
          <ul className="flex list-none flex-col gap-1 pl-0">
            {TASK_STATUSES.map((s) => (
              <li key={s}>
                <label className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent-subtle">
                  <Checkbox
                    aria-label={STATUS_LABELS[s]}
                    checked={filters.statuses.includes(s)}
                    onCheckedChange={() => toggleStatus(s)}
                  />
                  {STATUS_LABELS[s]}
                </label>
              </li>
            ))}
          </ul>
        </PopoverContent>
      </Popover>

      <Popover>
        <PopoverTrigger aria-pressed={priorityActive} className={filterTriggerClass(priorityActive)}>
          优先级{priorityActive && ` · ${filters.priorities.length}`}
        </PopoverTrigger>
        <PopoverContent className="w-48 p-2" role="group" aria-label="按优先级筛选">
          <ul className="flex list-none flex-col gap-1 pl-0">
            {TASK_PRIORITIES.map((p) => (
              <li key={p}>
                <label className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent-subtle">
                  <Checkbox
                    aria-label={PRIORITY_LABELS[p]}
                    checked={filters.priorities.includes(p)}
                    onCheckedChange={() => togglePriority(p)}
                  />
                  {PRIORITY_LABELS[p]}
                </label>
              </li>
            ))}
          </ul>
        </PopoverContent>
      </Popover>

      <Select
        value={filters.assignee === '' ? ANY_ASSIGNEE : filters.assignee}
        onValueChange={(v) => onChange({ ...filters, assignee: v === ANY_ASSIGNEE ? '' : v })}
      >
        <SelectTrigger aria-label="按负责人筛选" className={filterTriggerClass(assigneeActive)}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ANY_ASSIGNEE}>所有人</SelectItem>
          <SelectItem value="none">未分配</SelectItem>
          {users.map((u) => (
            <SelectItem key={u.id} value={u.id}>{u.name}</SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Popover>
        <PopoverTrigger aria-pressed={labelActive} className={filterTriggerClass(labelActive)}>
          {selectedLabel ? selectedLabel.Name : '标签'}
        </PopoverTrigger>
        <PopoverContent className="w-56 p-2">
          <ul className="flex list-none flex-col gap-1 pl-0" role="group" aria-label="按标签筛选">
            {labels.map((l) => (
              <li key={l.ID}>
                <label className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent-subtle">
                  <Checkbox
                    aria-label={l.Name}
                    checked={filters.labelId === l.ID}
                    onCheckedChange={() => toggleLabel(l.ID)}
                  />
                  {l.Name}
                </label>
              </li>
            ))}
          </ul>
          <div className="mt-2 border-t border-border pt-2">
            <LabelManager labels={labels} onChanged={onLabelsChanged} />
          </div>
        </PopoverContent>
      </Popover>

      <Select value={filters.sort} onValueChange={(sort) => onChange({ ...filters, sort })}>
        <SelectTrigger aria-label="排序字段" className={CONTROL_TRIGGER_CLASS}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {SORT_OPTIONS.map(([value, label]) => (
            <SelectItem key={value} value={value}>{label}</SelectItem>
          ))}
        </SelectContent>
      </Select>

      <button
        type="button"
        onClick={() => onChange({ ...filters, order: filters.order === 'asc' ? 'desc' : 'asc' })}
        aria-label={filters.order === 'asc' ? '升序，点击切换为降序' : '降序，点击切换为升序'}
        className={cn(CONTROL_TRIGGER_CLASS, 'justify-center')}
      >
        {filters.order === 'asc' ? '↑ 升序' : '↓ 降序'}
      </button>

      {/* Not on a phone. There it is the third way to reach the same capture
       * row — which sits directly above this bar, and which the bottom tab
       * bar's 新建 tab already jumps focus into — and it wraps onto a fourth
       * filter row of its own, ~52px of pure duplication on the tier with
       * the least vertical room. Every other tier keeps it. */}
      {tier !== 'phone' && (
        <button
          type="button"
          onClick={onRequestCreate}
          className={cn(
            CONTROL_TRIGGER_CLASS,
            'ml-auto justify-center border-accent bg-accent px-3 text-accent-fg hover:opacity-90',
          )}
        >
          <Plus className="size-3.5" aria-hidden="true" />
          新建任务
        </button>
      )}
    </div>
  )
}
