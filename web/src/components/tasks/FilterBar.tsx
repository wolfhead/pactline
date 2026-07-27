import { forwardRef, useImperativeHandle, useRef } from 'react'
import { useIdentity } from '../../identity'
import {
  PRIORITY_LABELS,
  STATUS_LABELS,
  TASK_PRIORITIES,
  TASK_STATUSES,
  type Label,
  type TaskPriority,
  type TaskStatus,
} from '../../task-types'
import LabelManager from './LabelManager'

export interface TaskFilters {
  statuses: TaskStatus[]
  priorities: TaskPriority[]
  assignee: string // '' = anyone, 'none' = unassigned, else a user id
  labelId: string // '' = any label
  search: string
  sort: string
  order: 'asc' | 'desc'
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

export interface FilterBarHandle {
  focusSearch: () => void
}

interface FilterBarProps {
  filters: TaskFilters
  onChange: (next: TaskFilters) => void
  labels: Label[]
  onLabelsChanged: (labels: Label[]) => void
}

/** Every filter is independent and additive — toggling one never clears
 * another, since "filters combine" is the whole point of a list view over
 * a fixed dataset. */
const FilterBar = forwardRef<FilterBarHandle, FilterBarProps>(function FilterBar(
  { filters, onChange, labels, onLabelsChanged },
  ref,
) {
  const { users } = useIdentity()
  const searchRef = useRef<HTMLInputElement>(null)

  useImperativeHandle(ref, () => ({
    focusSearch: () => searchRef.current?.focus(),
  }))

  function toggleStatus(s: TaskStatus) {
    const has = filters.statuses.includes(s)
    onChange({ ...filters, statuses: has ? filters.statuses.filter((x) => x !== s) : [...filters.statuses, s] })
  }

  function togglePriority(p: TaskPriority) {
    const has = filters.priorities.includes(p)
    onChange({ ...filters, priorities: has ? filters.priorities.filter((x) => x !== p) : [...filters.priorities, p] })
  }

  return (
    <div className="filter-bar">
      <input
        ref={searchRef}
        className="search-input"
        value={filters.search}
        onChange={(e) => onChange({ ...filters, search: e.target.value })}
        placeholder="搜索标题或描述…（按 / 聚焦）"
        aria-label="搜索任务"
      />

      <div className="filter-group" role="group" aria-label="按状态筛选">
        {TASK_STATUSES.map((s) => (
          <button
            key={s}
            type="button"
            className={`tag filter-chip ${filters.statuses.includes(s) ? 'active' : ''}`}
            onClick={() => toggleStatus(s)}
            aria-pressed={filters.statuses.includes(s)}
          >
            {STATUS_LABELS[s]}
          </button>
        ))}
      </div>

      <div className="filter-group" role="group" aria-label="按优先级筛选">
        {TASK_PRIORITIES.map((p) => (
          <button
            key={p}
            type="button"
            className={`tag filter-chip ${filters.priorities.includes(p) ? 'active' : ''}`}
            onClick={() => togglePriority(p)}
            aria-pressed={filters.priorities.includes(p)}
          >
            {PRIORITY_LABELS[p]}
          </button>
        ))}
      </div>

      <label>
        负责人
        <select
          value={filters.assignee}
          onChange={(e) => onChange({ ...filters, assignee: e.target.value })}
          aria-label="按负责人筛选"
        >
          <option value="">所有人</option>
          <option value="none">未分配</option>
          {users.map((u) => (
            <option key={u.id} value={u.id}>{u.name}</option>
          ))}
        </select>
      </label>

      <label>
        标签
        <select
          value={filters.labelId}
          onChange={(e) => onChange({ ...filters, labelId: e.target.value })}
          aria-label="按标签筛选"
        >
          <option value="">所有标签</option>
          {labels.map((l) => (
            <option key={l.ID} value={l.ID}>{l.Name}</option>
          ))}
        </select>
      </label>

      <label>
        排序
        <select
          value={filters.sort}
          onChange={(e) => onChange({ ...filters, sort: e.target.value })}
          aria-label="排序字段"
        >
          {SORT_OPTIONS.map(([v, l]) => (
            <option key={v} value={v}>{l}</option>
          ))}
        </select>
      </label>
      <button
        type="button"
        onClick={() => onChange({ ...filters, order: filters.order === 'asc' ? 'desc' : 'asc' })}
        aria-label={filters.order === 'asc' ? '升序，点击切换为降序' : '降序，点击切换为升序'}
      >
        {filters.order === 'asc' ? '↑ 升序' : '↓ 降序'}
      </button>

      <LabelManager labels={labels} onChanged={onLabelsChanged} />
    </div>
  )
})

export default FilterBar
