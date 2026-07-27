import { forwardRef, useImperativeHandle, useRef, useState, type FormEvent } from 'react'
import { createTask } from '../../api/tasks'
import type { Task } from '../../task-types'

export interface InlineCreateRowHandle {
  focus: () => void
}

interface InlineCreateRowProps {
  onCreated: (task: Task) => void
}

/**
 * Capture is instant: type a title, press Enter, the task exists and the
 * input is immediately ready for the next one. Everything else about a
 * task is set later, from the detail view. Exposes `focus()` via ref so a
 * global keyboard shortcut ("c") can jump straight into it from anywhere
 * on the page.
 */
const InlineCreateRow = forwardRef<InlineCreateRowHandle, InlineCreateRowProps>(function InlineCreateRow(
  { onCreated },
  ref,
) {
  const [title, setTitle] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useImperativeHandle(ref, () => ({
    focus: () => inputRef.current?.focus(),
  }))

  async function submit(e: FormEvent) {
    e.preventDefault()
    const trimmed = title.trim()
    if (!trimmed || pending) return
    setPending(true)
    setError('')
    try {
      const created = await createTask({ title: trimmed })
      onCreated(created)
      setTitle('')
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setPending(false)
      inputRef.current?.focus()
    }
  }

  return (
    <form className="quick-add" onSubmit={submit}>
      <span className="quick-add-mark" aria-hidden="true">+</span>
      <input
        ref={inputRef}
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        placeholder="输入标题，回车创建任务…"
        aria-label="新建任务标题"
        disabled={pending}
      />
      {error && <span className="error">{error}</span>}
    </form>
  )
})

export default InlineCreateRow
