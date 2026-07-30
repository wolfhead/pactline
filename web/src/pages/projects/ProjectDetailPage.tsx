import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { flushSync } from 'react-dom'
import { Link, NavLink, useParams, useSearchParams } from 'react-router-dom'
import { ChevronDown, ListChecks, Plus } from 'lucide-react'
import {
  applyMilestoneLifecycle,
  applyProjectArchive,
  createMilestone,
  createMilestoneCriterion,
  getProject,
  updateProject,
  updateMilestone,
  type Milestone,
  type ProjectActivity,
  type ProjectDetail,
} from '@/api/projects'
import {
  checkCriterion,
  removeCriterion,
  updateCriterion,
  type AcceptanceCriterion,
  type AcceptanceOutcome,
} from '@/api/acceptance'
import { ProblemError } from '@/api/v1/client'
import AcceptanceChecklist from '@/components/projects/AcceptanceChecklist'
import TaskCollection from '@/components/tasks/TaskCollection'
import TaskInspector from '@/components/tasks/TaskInspector'
import { useTaskCollection } from '@/components/tasks/useTaskCollection'
import { useTaskComposer } from '@/components/tasks/TaskComposer'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { useIdentity } from '@/identity'
import type { UserRef } from '@/task-types'
import { cn } from '@/lib/utils'

type ProjectView = 'overview' | 'milestones' | 'backlog'

const MILESTONE_LABELS = {
  planned: '计划中',
  active: '进行中',
  completed: '已完成',
  cancelled: '已取消',
} as const

const ACTIVITY_LABELS: Record<string, string> = {
  archived: '归档了项目',
  restored: '恢复了项目',
  project_name_changed: '修改了项目名称',
  project_description_changed: '修改了项目说明',
  project_owner_changed: '修改了项目负责人',
  milestone_updated: '更新了里程碑',
  milestone_activated: '激活了里程碑',
  milestone_completed: '完成了里程碑',
  milestone_cancelled: '取消了里程碑',
  milestone_reopened: '重新开启了里程碑',
}

export default function ProjectDetailPage({ view = 'overview' }: { view?: ProjectView }) {
  const number = Number(useParams().number)
  const selectedMilestoneID = useParams().milestoneID
  const { me, users, actor, impersonation } = useIdentity()
  const [detail, setDetail] = useState<ProjectDetail | null>(null)
  const [error, setError] = useState('')
  const [addingMilestone, setAddingMilestone] = useState(false)
  const [projectDetailsOpen, setProjectDetailsOpen] = useState(false)
  const [editingProject, setEditingProject] = useState(false)
  const [mutationPending, setMutationPending] = useState(false)
  const canAdminister = actor?.platform_role === 'ADMIN' && !impersonation

  const reload = useCallback(async () => {
    setError('')
    try {
      const loaded = await getProject(number)
      // Dependent versioned actions may become clickable as soon as reload
      // resolves. Commit the new aggregate versions before returning so a
      // fast follow-up action never submits the stale ETags from the previous
      // render.
      flushSync(() => setDetail(loaded))
    } catch (reason) {
      setError((reason as Error).message)
    }
  }, [number])

  useEffect(() => { void reload() }, [reload, me?.id])

  async function mutate(operation: () => Promise<unknown>): Promise<boolean> {
    setError('')
    setMutationPending(true)
    try {
      await operation()
      await reload()
      return true
    } catch (reason) {
      if (reason instanceof ProblemError && reason.code === 'VERSION_CONFLICT') {
        await reload()
        setError('内容已被其他用户或 Agent 更新，已加载最新版本。')
        return false
      }
      setError((reason as Error).message)
      return false
    } finally {
      setMutationPending(false)
    }
  }

  if (!detail && !error) return <p className="p-6 text-sm text-fg-muted">正在加载项目…</p>
  if (!detail) return <p role="alert" className="p-6 text-sm text-danger">加载失败：{error}</p>

  const { project } = detail
  const base = `/projects/${project.number}`
  const selectedMilestone = selectedMilestoneID
    ? detail.milestones.find((item) => item.id === selectedMilestoneID)
    : undefined

  return (
    <div className="flex min-h-full w-full flex-col">
      <header className="shrink-0 border-b border-border bg-surface px-4 pt-3 sm:px-5">
        <div className="flex min-h-10 flex-wrap items-center gap-x-4 gap-y-2">
          <Link to="/projects" className="shrink-0 text-sm text-accent">← 项目</Link>
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="truncate text-lg font-semibold">{project.name}</h1>
            {project.archived_at && <span className="shrink-0 rounded-full bg-surface-subtle px-2 py-1 text-xs">已归档</span>}
          </div>
          <nav aria-label="项目视图" className="order-last flex w-full gap-1 sm:order-none sm:w-auto">
          {([
            ['overview', '整体视图'],
            ['milestones', '里程碑'],
            ['backlog', 'Backlog'],
          ] as const).map(([key, label]) => (
            <NavLink
              key={key}
              to={`${base}/${key}`}
              className={({ isActive }) => cn(
                '-mb-px border-b-2 px-3 py-2.5 text-sm',
                isActive ? 'border-accent font-medium text-accent' : 'border-transparent text-fg-muted',
              )}
            >
              {label}
            </NavLink>
          ))}
          </nav>
          <button
            type="button"
            aria-expanded={projectDetailsOpen}
            aria-controls="project-details"
            onClick={() => {
              setProjectDetailsOpen((value) => {
                if (value) setEditingProject(false)
                return !value
              })
            }}
            className="ml-auto flex shrink-0 items-center gap-1 rounded-md px-2.5 py-1.5 text-sm text-fg-muted hover:bg-surface-subtle hover:text-fg"
          >
            项目详情
            <ChevronDown
              className={cn('size-4 transition-transform', projectDetailsOpen && 'rotate-180')}
              aria-hidden="true"
            />
          </button>
        </div>

        {projectDetailsOpen && (
          <div id="project-details" className="border-t border-border py-4">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div className="max-w-3xl">
                <p className="text-sm leading-6 text-fg-muted">{project.description || '暂无项目说明'}</p>
                <p className="mt-2 text-xs text-fg-subtle">
                  项目 #{project.number} · 负责人：{project.owner.name}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={mutationPending}
                  onClick={() => setEditingProject((value) => !value)}
                  className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:cursor-wait disabled:opacity-50"
                >
                  编辑项目
                </button>
                {canAdminister && (
                  <button
                    type="button"
                    disabled={mutationPending}
                    onClick={() => void mutate(() => applyProjectArchive(number, project.version, !project.archived_at))}
                    className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:cursor-wait disabled:opacity-50"
                  >
                    {project.archived_at ? '恢复项目' : '归档项目'}
                  </button>
                )}
              </div>
            </div>
            {editingProject && (
              <form
                className="mt-4 grid gap-3 rounded-lg bg-surface-subtle p-4 md:grid-cols-2"
                onSubmit={async (event) => {
                  event.preventDefault()
                  const data = new FormData(event.currentTarget)
                  const updated = await mutate(() => updateProject(project.number, project.version, {
                    name: String(data.get('name') ?? ''),
                    description: String(data.get('description') ?? ''),
                    owner_id: String(data.get('owner_id') ?? ''),
                  }))
                  if (updated) setEditingProject(false)
                }}
              >
                <input
                  name="name"
                  required
                  defaultValue={project.name}
                  aria-label="项目名称"
                  className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
                />
                <select
                  name="owner_id"
                  defaultValue={project.owner.id}
                  aria-label="项目负责人"
                  className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
                >
                  {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
                </select>
                <textarea
                  name="description"
                  defaultValue={project.description}
                  aria-label="项目说明"
                  rows={3}
                  className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm md:col-span-2"
                />
                <div className="flex justify-end md:col-span-2">
                  <button className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-white">保存</button>
                </div>
              </form>
            )}
          </div>
        )}
        {error && <p role="alert" className="border-t border-border py-2 text-sm text-danger">操作失败：{error}</p>}
      </header>

      <div className={cn(
        'flex min-h-0 flex-1 flex-col gap-4 p-4 sm:p-5',
        view === 'overview' && 'mx-auto w-full max-w-7xl',
      )}>
        {view === 'overview' && <Overview detail={detail} users={users} />}
        {view === 'backlog' && <Backlog detail={detail} users={users} />}
        {view === 'milestones' && (
          <Milestones
            detail={detail}
            selected={selectedMilestone}
            adding={addingMilestone}
            setAdding={setAddingMilestone}
            users={users}
            onMutate={mutate}
            onReload={reload}
          />
        )}
      </div>
    </div>
  )
}

function Overview({ detail, users }: { detail: ProjectDetail; users: Array<{ id: string; name: string }> }) {
  const today = new Date().toISOString().slice(0, 10)
  const liveTasks = detail.tasks.filter((task) => !task.archived_at)
  const unfinished = liveTasks.filter((task) => !['done', 'cancelled'].includes(task.status))
  const active = detail.milestones.filter((item) => item.status === 'active')
  const activeIDs = new Set(active.map((item) => item.id))
  const attention = [
    ['逾期里程碑', active.filter((item) => item.target_date && item.target_date < today).length],
    ['逾期任务', unfinished.filter((task) => task.due_date && task.due_date < today).length],
    ['活跃里程碑中的未分配任务', unfinished.filter((task) => !task.assignee && task.milestone && activeIDs.has(task.milestone.id)).length],
    ['待评审任务', liveTasks.filter((task) => task.status === 'in_review').length],
    ['任务已结束但验收未完成的里程碑', active.filter((milestone) => {
      const tasks = liveTasks.filter((task) => task.milestone?.id === milestone.id)
      const tasksConcluded = tasks.length > 0 && tasks.every((task) => ['done', 'cancelled'].includes(task.status))
      const acceptanceIncomplete = milestone.acceptance_criteria.some((criterion) => !['passed', 'waived'].includes(criterion.current_check?.outcome ?? ''))
      return tasksConcluded && acceptanceIncomplete
    }).length],
    ['高优先级 Backlog', liveTasks.filter((task) => !task.milestone && ['high', 'urgent'].includes(task.priority) && !['done', 'cancelled'].includes(task.status)).length],
  ] as const
  const backlogCount = liveTasks.filter((task) => !task.milestone).length

  return (
    <>
      <section>
        <h2 className="mb-3 text-lg font-semibold">需要关注</h2>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {attention.map(([label, count]) => (
            <div key={label} className={cn('rounded-lg border p-4', count ? 'border-warning/40 bg-warning/5' : 'border-border bg-surface-raised')}>
              <p className="text-sm text-fg-muted">{label}</p>
              <p className="mt-2 text-2xl font-semibold">{count}</p>
            </div>
          ))}
        </div>
      </section>
      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold">活跃里程碑</h2>
          <span className="text-sm text-fg-muted">Backlog {backlogCount} 项</span>
        </div>
        {active.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">当前没有活跃里程碑。</div>
        ) : (
          <div className="grid gap-3 lg:grid-cols-2">
            {active.map((milestone) => {
              const tasks = liveTasks.filter((task) => task.milestone?.id === milestone.id)
              const concluded = tasks.filter((task) => ['done', 'cancelled'].includes(task.status)).length
              const inReview = tasks.filter((task) => task.status === 'in_review').length
              const overdue = tasks.filter(
                (task) => !['done', 'cancelled'].includes(task.status)
                  && Boolean(task.due_date && task.due_date < today),
              ).length
              const satisfied = milestone.acceptance_criteria.filter((criterion) => ['passed', 'waived'].includes(criterion.current_check?.outcome ?? '')).length
              return (
                <Link key={milestone.id} to={`/projects/${detail.project.number}/milestones/${milestone.id}`} className="rounded-lg border border-border bg-surface-raised p-4 hover:border-accent/40">
                  <h3 className="font-semibold">{milestone.name}</h3>
                  <p className="mt-1 text-sm text-fg-muted">{milestone.outcome}</p>
                  <div className="mt-3 flex flex-wrap gap-3 text-xs text-fg-muted">
                    <span>负责人：{users.find((user) => user.id === milestone.owner_id)?.name ?? '未知'}</span>
                    <span>目标：{milestone.target_date ?? '未设置'}</span>
                    <span>任务：{concluded}/{tasks.length}</span>
                    <span>待评审：{inReview}</span>
                    <span>逾期：{overdue}</span>
                    <span>验收：{satisfied}/{milestone.acceptance_criteria.length}</span>
                  </div>
                </Link>
              )
            })}
          </div>
        )}
      </section>
      <ActivityFeed activity={detail.activity} users={users} milestones={detail.milestones} />
    </>
  )
}

function Backlog({
  detail,
  users,
}: {
  detail: ProjectDetail
  users: UserRef[]
}) {
  return (
    <section className="min-h-[32rem] flex-1 overflow-hidden rounded-xl border border-border bg-surface">
      <div className="border-b border-border px-4 py-3">
        <div>
          <h2 className="font-semibold">项目 Backlog</h2>
          <p className="mt-1 text-sm text-fg-muted">尚未安排到里程碑的任务。</p>
        </div>
      </div>
      <ProjectTaskCollection
        projectNumber={detail.project.number}
        backlogOnly
        users={users}
        empty="Backlog 为空。"
      />
    </section>
  )
}

function Milestones({
  detail, selected, adding, setAdding, users, onMutate, onReload,
}: {
  detail: ProjectDetail
  selected?: Milestone
  adding: boolean
  setAdding: (value: boolean) => void
  users: UserRef[]
  onMutate: (operation: () => Promise<unknown>) => Promise<boolean>
  onReload: () => Promise<void>
}) {
  const project = detail.project
  const [editingMilestone, setEditingMilestone] = useState(false)
  const [milestoneDetailsOpen, setMilestoneDetailsOpen] = useState(false)
  const [acceptanceOpen, setAcceptanceOpen] = useState(false)
  const [acceptanceMutationPending, setAcceptanceMutationPending] = useState(false)
  const selectedTasks = selected
    ? detail.tasks.filter((task) => !task.archived_at && task.milestone?.id === selected.id)
    : []
  const concludedTasks = selectedTasks.filter((task) => ['done', 'cancelled'].includes(task.status))
  const tasksConcluded = selectedTasks.length > 0 && concludedTasks.length === selectedTasks.length
  const acceptanceSatisfied = selected?.acceptance_criteria.filter((criterion) => (
    ['passed', 'waived'].includes(criterion.current_check?.outcome ?? '')
  )).length ?? 0

  useEffect(() => {
    setEditingMilestone(false)
    setMilestoneDetailsOpen(false)
    setAcceptanceOpen(false)
    setAcceptanceMutationPending(false)
  }, [selected?.id])
  useEffect(() => {
    if (selected?.status === 'completed' || tasksConcluded) setAcceptanceOpen(true)
  }, [selected?.status, tasksConcluded])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    await onMutate(() => createMilestone(project.number, project.version, {
      name: String(data.get('name') ?? ''),
      outcome: String(data.get('outcome') ?? ''),
      owner_id: String(data.get('owner_id') ?? ''),
      target_date: String(data.get('target_date') ?? '') || null,
      position: detail.milestones.length,
    }))
    setAdding(false)
  }

  if (selected) {
    const mutateAcceptance = async (operation: () => Promise<unknown>) => {
      setAcceptanceMutationPending(true)
      try {
        await operation()
        await onReload()
      } finally {
        setAcceptanceMutationPending(false)
      }
    }
    const lifecycle = async (action: 'activate' | 'complete' | 'cancel' | 'reopen') => {
      const reason = action === 'reopen' ? window.prompt('请输入重新开启原因') ?? undefined : undefined
      if (action === 'reopen' && !reason) return
      await onMutate(() => applyMilestoneLifecycle(project.number, project.version, selected.id, selected.version, action, reason))
    }
    return (
      <section className="flex min-h-0 flex-1 flex-col gap-3">
        <div className="rounded-lg border border-border bg-surface px-4 py-3">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <Link to={`/projects/${project.number}/milestones`} className="shrink-0 text-sm text-accent">
              ← 里程碑
            </Link>
            <h2 className="min-w-0 flex-1 truncate text-base font-semibold">{selected.name}</h2>
            <span className="shrink-0 text-xs text-fg-muted">
              {concludedTasks.length}/{selectedTasks.length} 完成
            </span>
            <span className="shrink-0 text-xs text-fg-muted">
              目标 {selected.target_date ?? '未设置'}
            </span>
            <button
              type="button"
              aria-expanded={milestoneDetailsOpen}
              aria-controls="milestone-details"
              onClick={() => {
                setMilestoneDetailsOpen((value) => {
                  if (value) setEditingMilestone(false)
                  return !value
                })
              }}
              className="flex shrink-0 items-center gap-1 rounded-md px-2.5 py-1.5 text-sm text-fg-muted hover:bg-surface-subtle hover:text-fg"
            >
              里程碑详情
              <ChevronDown
                className={cn('size-4 transition-transform', milestoneDetailsOpen && 'rotate-180')}
                aria-hidden="true"
              />
            </button>
          </div>

          {milestoneDetailsOpen && (
            <div id="milestone-details" className="mt-3 border-t border-border pt-4">
              <div className="flex flex-wrap items-start justify-between gap-4">
                <div className="max-w-3xl">
                  <p className="text-sm font-medium text-fg">{selected.outcome}</p>
                  {selected.description && (
                    <p className="mt-2 text-sm leading-6 text-fg-muted">{selected.description}</p>
                  )}
                  <p className="mt-2 text-xs text-fg-subtle">
                    负责人：{users.find((user) => user.id === selected.owner_id)?.name ?? '未知'}
                    {' · '}
                    状态：{MILESTONE_LABELS[selected.status]}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setEditingMilestone((value) => !value)}
                    className="rounded-md border border-border-strong px-3 py-1.5 text-sm"
                  >
                    编辑里程碑
                  </button>
                  {selected.status === 'planned' && (
                    <button disabled={acceptanceMutationPending} onClick={() => void lifecycle('activate')} className="rounded-md bg-accent px-3 py-1.5 text-sm text-white disabled:cursor-wait disabled:opacity-50">
                      激活
                    </button>
                  )}
                  {selected.status === 'active' && (
                    <button disabled={acceptanceMutationPending} onClick={() => void lifecycle('complete')} className="rounded-md bg-accent px-3 py-1.5 text-sm text-white disabled:cursor-wait disabled:opacity-50">
                      完成
                    </button>
                  )}
                  {['planned', 'active'].includes(selected.status) && (
                    <button disabled={acceptanceMutationPending} onClick={() => void lifecycle('cancel')} className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:cursor-wait disabled:opacity-50">
                      取消
                    </button>
                  )}
                  {['completed', 'cancelled'].includes(selected.status) && (
                    <button disabled={acceptanceMutationPending} onClick={() => void lifecycle('reopen')} className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:cursor-wait disabled:opacity-50">
                      重新开启
                    </button>
                  )}
                </div>
              </div>

              {editingMilestone && (
                <form
                  className="mt-4 grid gap-3 rounded-lg bg-surface-subtle p-4 md:grid-cols-2"
                  onSubmit={async (event) => {
                    event.preventDefault()
                    const data = new FormData(event.currentTarget)
                    const updated = await onMutate(() => updateMilestone(
                      project.number,
                      project.version,
                      selected.id,
                      selected.version,
                      {
                        name: String(data.get('name') ?? ''),
                        outcome: String(data.get('outcome') ?? ''),
                        description: String(data.get('description') ?? ''),
                        owner_id: String(data.get('owner_id') ?? ''),
                        target_date: String(data.get('target_date') ?? '') || null,
                      },
                    ))
                    if (updated) setEditingMilestone(false)
                  }}
                >
                  <input name="name" required defaultValue={selected.name} aria-label="里程碑名称" className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm" />
                  <select name="owner_id" defaultValue={selected.owner_id} aria-label="里程碑负责人" className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm">
                    {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
                  </select>
                  <textarea name="outcome" required defaultValue={selected.outcome} aria-label="里程碑成果" className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm md:col-span-2" />
                  <textarea name="description" defaultValue={selected.description} aria-label="里程碑说明" className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm md:col-span-2" />
                  <input name="target_date" type="date" defaultValue={selected.target_date ?? ''} aria-label="里程碑目标日期" className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm" />
                  <button className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">保存</button>
                </form>
              )}

              <div className="mt-5">
                <ActivityFeed
                  activity={detail.activity.filter((item) => item.milestone_id === selected.id)}
                  users={users}
                  milestones={detail.milestones}
                  title="最近动态"
                />
              </div>
            </div>
          )}
        </div>

        <div className="overflow-hidden rounded-lg border border-border bg-surface">
          <button
            type="button"
            aria-expanded={acceptanceOpen}
            aria-controls="milestone-acceptance"
            onClick={() => setAcceptanceOpen((value) => !value)}
            className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-surface-subtle/70"
          >
            <ListChecks className="size-4 shrink-0 text-secondary" aria-hidden="true" />
            <span className="text-sm font-medium">验收 {acceptanceSatisfied}/{selected.acceptance_criteria.length}</span>
            <span className="min-w-0 flex-1 truncate text-xs text-fg-muted">
              {tasksConcluded || selected.status === 'completed'
                ? '任务已结束，可以逐项检查验收结果'
                : `尚有 ${selectedTasks.length - concludedTasks.length} 项任务未完成`}
            </span>
            <ChevronDown
              className={cn('size-4 shrink-0 text-fg-muted transition-transform', acceptanceOpen && 'rotate-180')}
              aria-hidden="true"
            />
          </button>
          {acceptanceOpen && (
            <div id="milestone-acceptance" className="border-t border-border p-3">
              <AcceptanceChecklist
                title="里程碑验收标准"
                criteria={selected.acceptance_criteria}
                onAdd={async (criterion, instructions) => {
                  await mutateAcceptance(() => createMilestoneCriterion(
                    project.number,
                    project.version,
                    selected.id,
                    selected.version,
                    {
                      criterion,
                      verification_instructions: instructions,
                      position: selected.acceptance_criteria.length,
                    },
                  ))
                }}
                onCheck={async (criterion: AcceptanceCriterion, outcome: AcceptanceOutcome, evidence: string) => {
                  await mutateAcceptance(() => checkCriterion(
                    criterion.id,
                    criterion.version,
                    criterion.revision,
                    outcome,
                    evidence,
                  ))
                }}
                onUpdate={async (criterion, text, instructions) => {
                  await mutateAcceptance(() => updateCriterion(
                    criterion.id,
                    criterion.version,
                    { criterion: text, verification_instructions: instructions },
                  ))
                }}
                onRemove={async (criterion) => {
                  const reason = selected.status === 'active' ? window.prompt('请输入调整验收范围的原因') ?? undefined : undefined
                  if (selected.status === 'active' && !reason) return
                  await mutateAcceptance(() => removeCriterion(criterion.id, criterion.version, reason))
                }}
              />
            </div>
          )}
        </div>

        <div className="min-h-[32rem] flex-1 overflow-hidden rounded-lg border border-border bg-surface">
          <ProjectTaskCollection
            projectNumber={project.number}
            milestoneID={selected.id}
            users={users}
            canCreate={selected.status === 'planned' || selected.status === 'active'}
            empty="还没有任务归入此里程碑。"
          />
        </div>
      </section>
    )
  }

  const groups = [
    ['active', '进行中'],
    ['planned', '计划中'],
    ['completed', '已完成'],
    ['cancelled', '已取消'],
  ] as const
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">里程碑</h2>
        <button type="button" onClick={() => setAdding(!adding)} className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">新建里程碑</button>
      </div>
      {adding && (
        <form onSubmit={submit} className="grid gap-3 rounded-lg border border-border bg-surface-raised p-4 md:grid-cols-2">
          <input name="name" required placeholder="里程碑名称" className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          <select name="owner_id" defaultValue={project.owner.id} className="rounded-md border border-border-strong bg-surface px-3 py-2">
            {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
          </select>
          <textarea name="outcome" required placeholder="可验证的阶段成果" className="rounded-md border border-border-strong bg-surface px-3 py-2 md:col-span-2" />
          <input name="target_date" type="date" className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          <button className="rounded-md bg-accent px-3 py-2 text-sm text-white">创建</button>
        </form>
      )}
      {groups.map(([status, label]) => {
        const items = detail.milestones.filter((item) => item.status === status)
        if (!items.length) return null
        return (
          <div key={status}>
            <h3 className="mb-2 text-sm font-medium text-fg-muted">{label} · {items.length}</h3>
            <div className="grid gap-3 lg:grid-cols-2">
              {items.map((milestone) => (
                <Link key={milestone.id} to={`/projects/${project.number}/milestones/${milestone.id}`} className="rounded-lg border border-border bg-surface-raised p-4 hover:border-accent/40">
                  <h4 className="font-semibold">{milestone.name}</h4>
                  <p className="mt-1 text-sm text-fg-muted">{milestone.outcome}</p>
                  <p className="mt-2 text-xs text-fg-muted">负责人：{users.find((user) => user.id === milestone.owner_id)?.name ?? '未知'} · 目标：{milestone.target_date ?? '未设置'}</p>
                </Link>
              ))}
            </div>
          </div>
        )
      })}
    </section>
  )
}

function ActivityFeed({
  activity,
  users,
  milestones,
  title = '最近动态',
}: {
  activity: ProjectActivity[]
  users: Array<{ id: string; name: string }>
  milestones: Milestone[]
  title?: string
}) {
  const recent = [...activity].reverse().slice(0, 8)
  return (
    <section>
      <h2 className="mb-3 text-lg font-semibold">{title}</h2>
      {recent.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-5 text-sm text-fg-muted">暂无动态。</div>
      ) : (
        <ol className="divide-y divide-border rounded-lg border border-border bg-surface-raised">
          {recent.map((item) => {
            const actor = users.find((user) => user.id === item.actor_id)?.name ?? '未知用户'
            const milestone = item.milestone_id
              ? milestones.find((candidate) => candidate.id === item.milestone_id)?.name
              : undefined
            return (
              <li key={item.id} className="flex flex-wrap items-baseline gap-x-2 gap-y-1 px-4 py-3 text-sm">
                <span className="font-medium">{actor}</span>
                <span>{ACTIVITY_LABELS[item.action] ?? item.action}</span>
                {milestone && <span className="text-fg-muted">“{milestone}”</span>}
                {item.reason && <span className="text-fg-muted">原因：{item.reason}</span>}
                <time className="ml-auto text-xs text-fg-subtle" dateTime={item.created_at}>
                  {new Date(item.created_at).toLocaleString('zh-CN')}
                </time>
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}

function ProjectTaskCollection({
  projectNumber,
  milestoneID,
  backlogOnly = false,
  users,
  canCreate = true,
  empty,
}: {
  projectNumber: number
  milestoneID?: string
  backlogOnly?: boolean
  users: UserRef[]
  canCreate?: boolean
  empty: string
}) {
  const { me, isReadOnly } = useIdentity()
  const { openTaskComposer } = useTaskComposer()
  const tier = useBreakpoint()
  const [searchParams, setSearchParams] = useSearchParams()
  const query = useMemo(() => ({
    project_number: projectNumber,
    milestone_id: milestoneID,
    backlog_only: backlogOnly || undefined,
  }), [backlogOnly, milestoneID, projectNumber])
  const collection = useTaskCollection(query, me?.id)
  const selectedValue = Number(searchParams.get('task'))
  const selectedNumber = Number.isInteger(selectedValue) && selectedValue > 0
    ? selectedValue
    : null
  const selectedTask = selectedNumber === null
    ? null
    : collection.tasks.find((task) => task.number === selectedNumber) ?? null

  function taskHref(taskNumber: number) {
    const next = new URLSearchParams(searchParams)
    next.set('task', String(taskNumber))
    return `?${next.toString()}`
  }

  function closeTaskInspector() {
    const next = new URLSearchParams(searchParams)
    next.delete('task')
    setSearchParams(next, { replace: true })
  }

  return (
    <>
      <TaskCollection
        controller={collection}
        tier={tier}
        users={users}
        selectedNumber={selectedNumber}
        taskHref={(task) => taskHref(task.number)}
        allowGantt={!backlogOnly}
        empty={empty}
        actions={canCreate && !isReadOnly && (
          <button
            type="button"
            onClick={() => openTaskComposer({
              projectNumber,
              milestoneID,
              onCreated: collection.prependTask,
            })}
            className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-accent-fg shadow-[0_4px_12px_rgb(37_99_235/0.16)]"
          >
            <Plus className="size-3.5" aria-hidden="true" />
            新建任务
          </button>
        )}
      />
      <TaskInspector
        number={selectedNumber}
        users={users}
        syncedTask={selectedTask}
        onPatched={collection.replaceTask}
        onClose={closeTaskInspector}
      />
    </>
  )
}
