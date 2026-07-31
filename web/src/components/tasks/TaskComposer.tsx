import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
  type ReactNode,
} from 'react'
import { useNavigate } from 'react-router-dom'
import { createTask } from '@/api/tasks'
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

  const openTaskComposer = useCallback((next: OpenTaskComposerOptions = {}) => {
    if (isReadOnly) return
    setOptions(next)
    setProjectNumber(next.projectNumber ?? 0)
    setMilestoneID(next.milestoneID ?? '')
    setError('')
    setOpen(true)
  }, [isReadOnly])

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
    const data = new FormData(event.currentTarget)
    setPending(true)
    setError('')
    try {
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
      setOpen(false)
      if (options.onCreated) {
        options.onCreated(created)
      } else {
        navigate(`/tasks/${created.number}`)
      }
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setPending(false)
    }
  }

  const value = useMemo(() => ({ openTaskComposer }), [openTaskComposer])

  return (
    <TaskComposerContext.Provider value={value}>
      {children}
      <Sheet open={open} onOpenChange={(next) => !pending && setOpen(next)}>
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
                  disabled={loadingProjects || projects.length === 0}
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
                  disabled={!projectNumber || loadingMilestones}
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
                placeholder="为什么需要做这件事？当前遇到了什么问题？"
                className={inputClass}
              />
            </Field>
            <Field label="期望结果" required hint="描述完成后的可观察变化，不必在这里编写正式验收项。">
              <textarea
                name="expected_result"
                required
                rows={4}
                placeholder="完成后应该达到什么状态？"
                className={inputClass}
              />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="负责人">
                <select name="assignee_id" defaultValue="" className={inputClass}>
                  <option value="">暂不分配</option>
                  {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
                </select>
              </Field>
              <Field label="优先级">
                <select name="priority" defaultValue="none" className={inputClass}>
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
              <select name="execution_mode" defaultValue="human_only" className={inputClass}>
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
              onClick={() => setOpen(false)}
              className="rounded-md border border-border-strong px-4 py-2 text-sm disabled:opacity-50"
            >
              取消
            </button>
            <button
              type="submit"
              form="task-composer-form"
              disabled={pending || loadingProjects || projects.length === 0}
              className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg disabled:cursor-wait disabled:opacity-50"
            >
              {pending ? '正在创建…' : '创建任务'}
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
