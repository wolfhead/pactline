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
    <details className="label-manager">
      <summary>管理标签</summary>
      <ul className="label-manage-list">
        {labels.map((l) => (
          <li key={l.ID} className="row">
            <InlineEditable value={l.Name} onCommit={(next) => rename(l.ID, next)} ariaLabel={`重命名标签 ${l.Name}`} />
            <button type="button" onClick={() => remove(l.ID)}>删除</button>
          </li>
        ))}
      </ul>
      <form className="row" onSubmit={submitNew}>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="新标签名称" aria-label="新标签名称" />
        <button type="submit" disabled={!name.trim() || creating}>{creating ? '创建中…' : '新建标签'}</button>
      </form>
      {error && <span className="error">{error}</span>}
    </details>
  )
}
