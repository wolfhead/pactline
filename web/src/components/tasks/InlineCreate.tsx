import { forwardRef, useEffect, useImperativeHandle, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Plus } from 'lucide-react'
import { createTask } from '../../api/tasks'
import type { Task } from '../../task-types'

export interface InlineCreateHandle {
  focus(): void
}

interface InlineCreateProps {
  onCreated: (task: Task) => void
}

/**
 * Capture is instant: type a title, press Enter, the task exists and the
 * input is immediately ready for the next one. Everything else about a
 * task is set later, from the detail view. Exposes `focus()` via ref so the
 * filter bar's "＋ 新建任务" button (and the phone bottom bar's "新建" tab)
 * can jump straight into it without opening a dialog of its own.
 */
const InlineCreate = forwardRef<InlineCreateHandle, InlineCreateProps>(function InlineCreate(
  { onCreated },
  ref,
) {
  const [title, setTitle] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  // Set right before setPending(false) and consumed by the effect below —
  // see that effect's comment for why a synchronous .focus() here doesn't
  // work.
  const refocusPendingRef = useRef(false)

  useImperativeHandle(ref, () => ({
    focus: () => inputRef.current?.focus(),
  }))

  // Re-focuses the input once React has actually committed `disabled` back
  // to false. Calling .focus() synchronously in submit()'s `finally` block
  // targets an element that is, at that instant, still disabled (setPending
  // only *schedules* the re-render) — a silent no-op in every real browser.
  // Waiting for this effect to run after the `pending` commit is what makes
  // the input genuinely focusable again.
  useEffect(() => {
    if (!pending && refocusPendingRef.current) {
      refocusPendingRef.current = false
      inputRef.current?.focus()
    }
  }, [pending])

  async function submit() {
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
      refocusPendingRef.current = true
      setPending(false)
    }
  }

  function handleFormSubmit(e: FormEvent) {
    e.preventDefault()
    void submit()
  }

  // Enter is handled here rather than left to the form's implicit
  // submit-on-Enter: preventDefault on the keydown suppresses that native
  // behaviour, so this fires exactly once either way. jsdom (unlike every
  // real browser) never implements implicit form submission on Enter at
  // all, so relying on it alone would leave this untestable without a real
  // browser.
  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      void submit()
    }
  }

  return (
    <form
      className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 focus-within:border-accent"
      onSubmit={handleFormSubmit}
    >
      <Plus className="size-4 shrink-0 text-fg-subtle" aria-hidden="true" />
      <input
        ref={inputRef}
        className="w-full min-w-0 flex-1 border-0 bg-transparent p-0 text-sm text-fg outline-none placeholder:text-fg-muted disabled:opacity-50"
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="输入标题，回车创建任务…"
        aria-label="新建任务"
        disabled={pending}
      />
      {error && <span className="shrink-0 text-xs text-danger">{error}</span>}
    </form>
  )
})

export default InlineCreate
