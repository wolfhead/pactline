import { useState, type FormEvent } from 'react'
import { createLabel, deleteLabel, renameLabel } from '../../api/tasks'
import type { Label } from '../../task-types'
import InlineEditable from './InlineEditable'

interface LabelManagerProps {
  labels: Label[]
  onChanged: (labels: Label[]) => void
}

/**
 * Create, rename and delete labels — folded into the filter bar (as a
 * <details> disclosure) rather than given its own top-level nav entry,
 * since the only place a label is ever acted on is while filtering or
 * tagging a task.
 */
export default function LabelManager({ labels, onChanged }: LabelManagerProps) {
  const [name, setName] = useState('')
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')

  async function submitNew(e: FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed || creating) return
    setCreating(true)
    setError('')
    try {
      const created = await createLabel(trimmed)
      onChanged([...labels, created].sort((a, b) => a.Name.localeCompare(b.Name)))
      setName('')
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setCreating(false)
    }
  }

  function rename(id: string, next: string) {
    if (!next.trim()) return
    const previous = labels
    onChanged(labels.map((l) => (l.ID === id ? { ...l, Name: next } : l)))
    renameLabel(id, next).catch((err) => {
      onChanged(previous)
      setError(String((err as Error).message))
    })
  }

  function remove(id: string) {
    const previous = labels
    onChanged(labels.filter((l) => l.ID !== id))
    deleteLabel(id).catch((err) => {
      onChanged(previous)
      setError(String((err as Error).message))
    })
  }

  return (
    <details className="text-sm">
      <summary className="cursor-pointer text-fg-muted hover:text-fg">管理标签</summary>
      <ul className="my-2 flex list-none flex-col gap-1 pl-0">
        {labels.map((l) => (
          <li key={l.ID} className="flex items-center justify-between gap-2">
            <InlineEditable
              value={l.Name}
              onCommit={(next) => rename(l.ID, next)}
              ariaLabel={`重命名标签 ${l.Name}`}
              className="min-w-0 flex-1 rounded border-0 bg-transparent px-1 py-0.5 text-sm text-fg"
            />
            <button
              type="button"
              onClick={() => remove(l.ID)}
              className="shrink-0 text-xs text-fg-muted hover:text-danger"
            >
              删除
            </button>
          </li>
        ))}
      </ul>
      <form className="flex items-center gap-2" onSubmit={submitNew}>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="新标签名称"
          aria-label="新标签名称"
          className="min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-2 py-1 text-xs text-fg placeholder:text-fg-muted"
        />
        <button
          type="submit"
          disabled={!name.trim() || creating}
          className="shrink-0 rounded-md bg-accent px-2 py-1 text-xs font-medium text-accent-fg disabled:cursor-not-allowed disabled:opacity-50"
        >
          {creating ? '创建中…' : '新建标签'}
        </button>
      </form>
      {error && <span className="text-xs text-danger">{error}</span>}
    </details>
  )
}
