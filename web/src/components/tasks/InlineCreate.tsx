import { forwardRef, useEffect, useImperativeHandle, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Plus } from 'lucide-react'
import { createTask } from '../../api/tasks'
import { listProjects, type Project } from '../../api/projects'
import { useIdentity } from '../../identity'
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
  const { isReadOnly } = useIdentity()
  const [title, setTitle] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [projects, setProjects] = useState<Project[]>([])
  const [projectsLoaded, setProjectsLoaded] = useState(false)
  const [projectNumber, setProjectNumber] = useState<number>(() => {
    const stored = window.localStorage.getItem('task-capture-project')
    return stored ? Number(stored) : 0
  })
  const inputRef = useRef<HTMLInputElement>(null)
  const focusRequestedRef = useRef(false)

  useImperativeHandle(ref, () => ({
    focus: () => {
      focusRequestedRef.current = true
      if (!inputRef.current?.disabled) {
        focusRequestedRef.current = false
        inputRef.current?.focus()
      }
    },
  }))

  // A focus request can arrive while Projects are still loading or while a
  // task submission has disabled the input. Keep the request until React has
  // committed a genuinely focusable input instead of dropping the user's
  // click as a silent browser no-op.
  useEffect(() => {
    if (
      focusRequestedRef.current
      && !pending
      && !isReadOnly
      && projectsLoaded
      && projects.length > 0
    ) {
      focusRequestedRef.current = false
      inputRef.current?.focus()
    }
  }, [isReadOnly, pending, projects.length, projectsLoaded])

  useEffect(() => {
    let cancelled = false
    listProjects()
      .then((items) => {
        if (cancelled) return
        setProjects(items)
        if (!items.some((project) => project.number === projectNumber) && items[0]) {
          setProjectNumber(items[0].number)
          window.localStorage.setItem('task-capture-project', String(items[0].number))
        }
      })
      .catch((reason) => setError(String((reason as Error).message)))
      .finally(() => {
        if (!cancelled) setProjectsLoaded(true)
      })
    return () => { cancelled = true }
    // Project selection is initialized once. Subsequent selection changes are
    // local state and must not refetch the entire navigation catalog.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function submit() {
    const trimmed = title.trim()
    if (!trimmed || pending || !projectNumber) return
    setPending(true)
    setError('')
    try {
      const created = await createTask({ title: trimmed, project_number: projectNumber })
      onCreated(created)
      setTitle('')
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      focusRequestedRef.current = true
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
      className="flex items-center gap-2 rounded-md border border-border-strong bg-surface px-3 py-2 focus-within:border-accent"
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
        disabled={pending || isReadOnly || !projectsLoaded || projects.length === 0}
      />
      <select
        aria-label="所属项目"
        value={projectNumber || ''}
        onChange={(event) => {
          const value = Number(event.target.value)
          setProjectNumber(value)
          window.localStorage.setItem('task-capture-project', String(value))
        }}
        disabled={pending || isReadOnly || !projectsLoaded || projects.length === 0}
        className="max-w-40 border-0 bg-transparent text-xs text-fg-muted outline-none"
      >
        {projects.map((project) => <option key={project.id} value={project.number}>{project.name}</option>)}
      </select>
      {error && <span className="shrink-0 text-xs text-danger">{error}</span>}
    </form>
  )
})

export default InlineCreate
