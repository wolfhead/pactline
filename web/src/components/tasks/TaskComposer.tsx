import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type DragEvent,
  type FormEvent,
  type ReactNode,
  type RefObject,
} from 'react'
import { AlertCircle, CheckCircle2, LoaderCircle, Paperclip, Upload, X } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import {
  completeTaskAttachmentUploadVersioned,
  createTask,
  createTaskAttachmentUpload,
  uploadTaskAttachment,
} from '@/api/tasks'
import { getProject, listProjects, type Milestone, type Project } from '@/api/projects'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useIdentity } from '@/identity'
import type { Task, TaskPriority } from '@/task-types'

interface OpenTaskComposerOptions {
  projectNumber?: number
  milestoneID?: string
  onCreated?: (task: Task) => void
}

interface TaskComposerContextValue {
  openTaskComposer: (options?: OpenTaskComposerOptions) => void
}

const TaskComposerContext = createContext<TaskComposerContextValue | null>(null)

const PRIORITY_OPTIONS: Array<{ value: TaskPriority; label: string }> = [
  { value: 'none', label: '无优先级' },
  { value: 'low', label: '低' },
  { value: 'medium', label: '中' },
  { value: 'high', label: '高' },
  { value: 'urgent', label: '紧急' },
]

const MAX_ATTACHMENT_BYTES = 100 * 1024 * 1024
const MAX_ATTACHMENTS = 100

type StagedAttachmentStatus = 'ready' | 'uploading' | 'uploaded' | 'failed'

interface StagedAttachment {
  id: number
  file: File
  status: StagedAttachmentStatus
  error?: string
}

export function TaskComposerProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const { users, isReadOnly } = useIdentity()
  const [open, setOpen] = useState(false)
  const [options, setOptions] = useState<OpenTaskComposerOptions>({})
  const [projects, setProjects] = useState<Project[]>([])
  const [milestones, setMilestones] = useState<Milestone[]>([])
  const [projectNumber, setProjectNumber] = useState(0)
  const [milestoneID, setMilestoneID] = useState('')
  const [loadingProjects, setLoadingProjects] = useState(false)
  const [loadingMilestones, setLoadingMilestones] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [createdTask, setCreatedTask] = useState<Task | null>(null)
  const [stagedAttachments, setStagedAttachments] = useState<StagedAttachment[]>([])
  const [attachmentNotice, setAttachmentNotice] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)
  const nextAttachmentID = useRef(1)
  const reportedTaskNumber = useRef<number | null>(null)

  const openTaskComposer = useCallback((next: OpenTaskComposerOptions = {}) => {
    if (isReadOnly) return
    setOptions(next)
    setProjectNumber(next.projectNumber ?? 0)
    setMilestoneID(next.milestoneID ?? '')
    setError('')
    setCreatedTask(null)
    setStagedAttachments([])
    setAttachmentNotice('')
    nextAttachmentID.current = 1
    reportedTaskNumber.current = null
    setOpen(true)
  }, [isReadOnly])

  function reportCreated(task: Task, forceNavigate = false) {
    if (reportedTaskNumber.current !== task.number) {
      reportedTaskNumber.current = task.number
      options.onCreated?.(task)
    }
    if (forceNavigate || !options.onCreated) navigate(`/tasks/${task.number}`)
  }

  function closeComposer() {
    if (pending) return
    if (createdTask) reportCreated(createdTask)
    setOpen(false)
  }

  function openCreatedTask() {
    if (!createdTask || pending) return
    reportCreated(createdTask, true)
    setOpen(false)
  }

  function updateAttachment(id: number, patch: Partial<StagedAttachment>) {
    setStagedAttachments((current) => current.map((attachment) => (
      attachment.id === id ? { ...attachment, ...patch } : attachment
    )))
  }

  function addAttachments(files: File[]) {
    setAttachmentNotice('')
    setError('')
    const available = Math.max(0, MAX_ATTACHMENTS - stagedAttachments.length)
    const accepted = files.slice(0, available)
    const rejected = files.length - accepted.length
    const valid: StagedAttachment[] = []
    let invalid = 0
    for (const file of accepted) {
      if (file.size <= 0 || file.size > MAX_ATTACHMENT_BYTES) {
        invalid += 1
        continue
      }
      valid.push({
        id: nextAttachmentID.current++, file, status: 'ready',
      })
    }
    setStagedAttachments((current) => [...current, ...valid])
    if (invalid > 0 || rejected > 0) {
      const reasons = []
      if (invalid > 0) reasons.push(`${invalid} 个文件为空或超过 100 MiB`)
      if (rejected > 0) reasons.push(`最多只能添加 ${MAX_ATTACHMENTS} 个附件`)
      setAttachmentNotice(`未添加：${reasons.join('；')}。`)
    }
  }

  function selectAttachments(event: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.target.files ?? [])
    event.target.value = ''
    addAttachments(files)
  }

  function dropAttachments(event: DragEvent<HTMLDivElement>) {
    event.preventDefault()
    if (pending || createdTask) return
    addAttachments(Array.from(event.dataTransfer.files))
  }

  async function uploadAttachments(task: Task): Promise<Task | null> {
    let currentTask = task
    const candidates = stagedAttachments.filter(({ status }) => status !== 'uploaded')
    setAttachmentNotice('')
    for (const attachment of candidates) {
      updateAttachment(attachment.id, { status: 'uploading', error: undefined })
      try {
        const session = await createTaskAttachmentUpload(currentTask.number, attachment.file)
        await uploadTaskAttachment(session, attachment.file)
        const completed = await completeTaskAttachmentUploadVersioned(
          currentTask.number,
          session.id,
          currentTask.version,
        )
        currentTask = { ...currentTask, version: completed.taskVersion }
        setCreatedTask(currentTask)
        updateAttachment(attachment.id, { status: 'uploaded', error: undefined })
      } catch (reason) {
        const message = (reason as Error).message
        updateAttachment(attachment.id, { status: 'failed', error: message })
        setCreatedTask(currentTask)
        setAttachmentNotice(
          `任务 #${currentTask.number} 已创建，但附件上传未完成。请重试，或打开任务后继续添加。`,
        )
        return null
      }
    }
    return currentTask
  }

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoadingProjects(true)
    listProjects()
      .then((items) => {
        if (cancelled) return
        setProjects(items)
        setProjectNumber((current) => {
          if (items.some((project) => project.number === current)) return current
          const remembered = Number(window.localStorage.getItem('task-create-project'))
          if (items.some((project) => project.number === remembered)) return remembered
          return items[0]?.number ?? 0
        })
      })
      .catch((reason) => {
        if (!cancelled) setError((reason as Error).message)
      })
      .finally(() => {
        if (!cancelled) setLoadingProjects(false)
      })
    return () => { cancelled = true }
  }, [open])

  useEffect(() => {
    if (!open || !projectNumber) {
      setMilestones([])
      return
    }
    let cancelled = false
    setLoadingMilestones(true)
    getProject(projectNumber)
      .then((detail) => {
        if (cancelled) return
        const available = detail.milestones.filter((item) => (
          item.status === 'planned' || item.status === 'active'
        ))
        setMilestones(available)
        setMilestoneID((current) => (
          available.some((item) => item.id === current) ? current : ''
        ))
      })
      .catch((reason) => {
        if (!cancelled) {
          setMilestones([])
          setError((reason as Error).message)
        }
      })
      .finally(() => {
        if (!cancelled) setLoadingMilestones(false)
      })
    return () => { cancelled = true }
  }, [open, projectNumber])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (pending || !projectNumber) return
    setPending(true)
    setError('')
    try {
      if (createdTask) {
        const completedTask = await uploadAttachments(createdTask)
        if (completedTask) {
          reportCreated(completedTask)
          setOpen(false)
        }
        return
      }
      const data = new FormData(event.currentTarget)
      const assigneeID = String(data.get('assignee_id') ?? '')
      const created = await createTask({
        title: String(data.get('title') ?? '').trim(),
        context: String(data.get('context') ?? '').trim(),
        expected_result: String(data.get('expected_result') ?? '').trim(),
        project_number: projectNumber,
        milestone_id: milestoneID || null,
        assignee_id: assigneeID || null,
        priority: String(data.get('priority') ?? 'none') as TaskPriority,
        execution_mode: data.get('execution_mode') === 'agent_allowed'
          ? 'agent_allowed'
          : 'human_only',
      })
      window.localStorage.setItem('task-create-project', String(projectNumber))
      setCreatedTask(created)
      if (stagedAttachments.length > 0) {
        const completedTask = await uploadAttachments(created)
        if (completedTask) {
          reportCreated(completedTask)
          setOpen(false)
        }
      } else {
        reportCreated(created)
        setOpen(false)
      }
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setPending(false)
    }
  }

  const value = useMemo(() => ({ openTaskComposer }), [openTaskComposer])
  const hasPendingAttachments = stagedAttachments.some(({ status }) => status !== 'uploaded')

  return (
    <TaskComposerContext.Provider value={value}>
      {children}
      <Sheet open={open} onOpenChange={(next) => next ? setOpen(true) : closeComposer()}>
        <SheetContent side="right" className="w-full gap-0 overflow-y-auto p-0 sm:max-w-[34rem]">
          <SheetHeader className="border-b border-border px-5 py-5">
            <SheetTitle>新建任务</SheetTitle>
            <SheetDescription>
              先说明为什么需要做，以及完成后应产生什么结果。验收标准可以稍后补充。
            </SheetDescription>
          </SheetHeader>
          <form id="task-composer-form" onSubmit={submit} className="flex flex-1 flex-col gap-5 px-5 py-5">
            <Field label="标题" required>
              <input
                name="title"
                required
                autoFocus
                maxLength={500}
                disabled={createdTask !== null}
                placeholder="用一句简洁的行动描述概括任务"
                className={inputClass}
              />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="项目" required>
                <select
                  name="project_number"
                  required
                  value={projectNumber || ''}
                  disabled={createdTask !== null || loadingProjects || projects.length === 0}
                  onChange={(event) => {
                    const next = Number(event.target.value)
                    setProjectNumber(next)
                    setMilestoneID('')
                    setError('')
                  }}
                  className={inputClass}
                >
                  {projects.length === 0 && <option value="">暂无可用项目</option>}
                  {projects.map((project) => (
                    <option key={project.id} value={project.number}>{project.name}</option>
                  ))}
                </select>
              </Field>
              <Field label="里程碑">
                <select
                  name="milestone_id"
                  value={milestoneID}
                  disabled={createdTask !== null || !projectNumber || loadingMilestones}
                  onChange={(event) => setMilestoneID(event.target.value)}
                  className={inputClass}
                >
                  <option value="">项目 Backlog</option>
                  {milestones.map((milestone) => (
                    <option key={milestone.id} value={milestone.id}>{milestone.name}</option>
                  ))}
                </select>
              </Field>
            </div>
            <Field label="背景 / 问题" required hint="说明当前情况、触发原因或需要解决的问题。">
              <textarea
                name="context"
                required
                rows={4}
                disabled={createdTask !== null}
                placeholder="为什么需要做这件事？当前遇到了什么问题？"
                className={inputClass}
              />
            </Field>
            <Field label="期望结果" required hint="描述完成后的可观察变化，不必在这里编写正式验收项。">
              <textarea
                name="expected_result"
                required
                rows={4}
                disabled={createdTask !== null}
                placeholder="完成后应该达到什么状态？"
                className={inputClass}
              />
            </Field>
            <AttachmentPicker
              attachments={stagedAttachments}
              notice={attachmentNotice}
              disabled={pending || createdTask !== null}
              busy={pending}
              inputRef={fileInputRef}
              onSelect={selectAttachments}
              onDrop={dropAttachments}
              onRemove={(id) => setStagedAttachments((current) => (
                current.filter((attachment) => attachment.id !== id)
              ))}
            />
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="负责人">
                <select name="assignee_id" defaultValue="" disabled={createdTask !== null} className={inputClass}>
                  <option value="">暂不分配</option>
                  {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
                </select>
              </Field>
              <Field label="优先级">
                <select name="priority" defaultValue="none" disabled={createdTask !== null} className={inputClass}>
                  {PRIORITY_OPTIONS.map((priority) => (
                    <option key={priority.value} value={priority.value}>{priority.label}</option>
                  ))}
                </select>
              </Field>
            </div>
            <Field
              label="执行方式"
              hint="允许 Agent 执行后，只有分配给你的待办任务才能被你的 Codex session 领取。"
            >
              <select name="execution_mode" defaultValue="human_only" disabled={createdTask !== null} className={inputClass}>
                <option value="human_only">仅人工执行</option>
                <option value="agent_allowed">允许 Agent 执行</option>
              </select>
            </Field>
            {error && <p role="alert" className="text-sm text-danger">创建失败：{error}</p>}
          </form>
          <SheetFooter className="flex-row justify-end border-t border-border px-5 py-4">
            <button
              type="button"
              disabled={pending}
              onClick={() => createdTask ? openCreatedTask() : closeComposer()}
              className="rounded-md border border-border-strong px-4 py-2 text-sm disabled:opacity-50"
            >
              {createdTask ? '打开任务' : '取消'}
            </button>
            <button
              type="submit"
              form="task-composer-form"
              disabled={pending || loadingProjects || projects.length === 0}
              className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg disabled:cursor-wait disabled:opacity-50"
            >
              {pending
                ? (createdTask ? '正在上传附件…' : '正在创建…')
                : (createdTask
                    ? (hasPendingAttachments ? '重试上传' : '完成')
                    : '创建任务')}
            </button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </TaskComposerContext.Provider>
  )
}

export function useTaskComposer(): TaskComposerContextValue {
  const value = useContext(TaskComposerContext)
  if (!value) {
    throw new Error('useTaskComposer must be used within TaskComposerProvider')
  }
  return value
}

function AttachmentPicker({
  attachments,
  notice,
  disabled,
  busy,
  inputRef,
  onSelect,
  onDrop,
  onRemove,
}: {
  attachments: StagedAttachment[]
  notice: string
  disabled: boolean
  busy: boolean
  inputRef: RefObject<HTMLInputElement>
  onSelect: (event: ChangeEvent<HTMLInputElement>) => void
  onDrop: (event: DragEvent<HTMLDivElement>) => void
  onRemove: (id: number) => void
}) {
  return (
    <section className="flex flex-col gap-2" aria-labelledby="task-attachment-label">
      <div>
        <h3 id="task-attachment-label" className="text-sm font-medium">附件</h3>
        <p className="mt-0.5 text-xs text-fg-muted">随任务一起提交；单个文件不超过 100 MiB。</p>
      </div>
      <input
        ref={inputRef}
        type="file"
        multiple
        className="hidden"
        disabled={disabled}
        onChange={onSelect}
      />
      <div
        onDragOver={(event) => event.preventDefault()}
        onDrop={onDrop}
        className="flex min-h-20 items-center justify-center rounded-lg border border-dashed border-border-strong bg-surface-subtle px-4 py-3 text-center"
      >
        <div>
          <Paperclip className="mx-auto size-5 text-fg-muted" aria-hidden="true" />
          <button
            type="button"
            disabled={disabled || attachments.length >= MAX_ATTACHMENTS}
            onClick={() => inputRef.current?.click()}
            className="mt-1.5 inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm font-medium text-accent hover:bg-accent/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30 disabled:text-fg-muted disabled:opacity-60"
          >
            <Upload className="size-3.5" aria-hidden="true" />
            选择附件
          </button>
          <p className="mt-1 text-xs text-fg-muted">
            {busy
              ? '正在处理所选附件'
              : (disabled ? '任务已创建，可重试未完成的附件' : '也可以将文件拖到这里')}
          </p>
        </div>
      </div>

      {attachments.length > 0 && (
        <ul className="max-h-44 divide-y divide-border overflow-y-auto border-y border-border" aria-label="待上传附件">
          {attachments.map((attachment) => (
            <li key={attachment.id} className="flex items-center gap-2.5 py-2">
              <AttachmentStatusIcon status={attachment.status} />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm text-fg">{attachment.file.name}</p>
                <p className={attachment.status === 'failed' ? 'text-xs text-danger' : 'text-xs text-fg-muted'}>
                  {attachment.status === 'failed'
                    ? `上传失败：${attachment.error}`
                    : `${formatBytes(attachment.file.size)} · ${attachmentStatusLabel(attachment.status)}`}
                </p>
              </div>
              {attachment.status !== 'uploaded' && (
                <button
                  type="button"
                  disabled={busy || attachment.status === 'uploading'}
                  onClick={() => onRemove(attachment.id)}
                  className="rounded-md p-1.5 text-fg-muted hover:bg-surface hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30 disabled:opacity-40"
                  aria-label={`移除 ${attachment.file.name}`}
                >
                  <X className="size-4" aria-hidden="true" />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {notice && <p role="alert" className="text-sm text-danger">{notice}</p>}
    </section>
  )
}

function AttachmentStatusIcon({ status }: { status: StagedAttachmentStatus }) {
  if (status === 'uploaded') {
    return <CheckCircle2 className="size-4 shrink-0 text-success" aria-hidden="true" />
  }
  if (status === 'uploading') {
    return <LoaderCircle className="size-4 shrink-0 animate-spin text-accent" aria-hidden="true" />
  }
  if (status === 'failed') {
    return <AlertCircle className="size-4 shrink-0 text-danger" aria-hidden="true" />
  }
  return <Paperclip className="size-4 shrink-0 text-fg-muted" aria-hidden="true" />
}

function attachmentStatusLabel(status: StagedAttachmentStatus): string {
  if (status === 'uploading') return '正在上传'
  if (status === 'uploaded') return '已上传'
  if (status === 'failed') return '上传失败'
  return '等待上传'
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}

function Field({
  label,
  required,
  hint,
  children,
}: {
  label: string
  required?: boolean
  hint?: string
  children: ReactNode
}) {
  return (
    <label className="flex flex-col gap-1.5 text-sm font-medium">
      <span>
        {label}
        {required && <span className="ml-1 text-danger" aria-hidden="true">*</span>}
      </span>
      {children}
      {hint && <span className="text-xs font-normal text-fg-muted">{hint}</span>}
    </label>
  )
}

const inputClass = 'w-full rounded-md border border-border-strong bg-surface px-3 py-2 text-sm outline-none placeholder:text-fg-muted focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-50'
