import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { flushSync } from 'react-dom'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { ChevronDown, ListChecks, Plus } from 'lucide-react'
import {
  addProjectMember,
  applyMilestoneLifecycle,
  applyProjectArchive,
  createMilestone,
  createMilestoneCriterion,
  getProject,
  listProjectMembers,
  removeProjectMember,
  updateProject,
  updateProjectMember,
  updateMilestone,
  type Milestone,
  type ProjectActivity,
  type ProjectDetail,
  type ProjectMembership,
  type ProjectRole,
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
import ProjectMembersPanel from '@/components/projects/ProjectMembersPanel'
import ProjectAgentConversationsPanel from '@/components/projects/ProjectAgentConversationsPanel'
import ProjectRepositoryAccessPanel from '@/components/projects/ProjectRepositoryAccessPanel'
import TaskCollection from '@/components/tasks/TaskCollection'
import { useTaskCollection } from '@/components/tasks/useTaskCollection'
import {
  searchParamsWithTaskFilters,
  searchParamsWithTaskPageCount,
  taskFiltersFromSearchParams,
  taskPageCountFromSearchParams,
} from '@/components/tasks/task-collection-search'
import { useTaskComposer } from '@/components/tasks/TaskComposer'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { useIdentity } from '@/identity'
import type { UserRef } from '@/task-types'
import { cn } from '@/lib/utils'

type ProjectView = 'workspace' | 'milestone'

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
  project_membership_add: '添加了项目成员',
  project_membership_change: '调整了项目成员角色',
  project_membership_remove: '移除了项目成员',
  milestone_updated: '更新了里程碑',
  milestone_activated: '激活了里程碑',
  milestone_completed: '完成了里程碑',
  milestone_cancelled: '取消了里程碑',
  milestone_reopened: '重新开启了里程碑',
}

export default function ProjectDetailPage({ view = 'workspace' }: { view?: ProjectView }) {
  const number = Number(useParams().number)
  const selectedMilestoneID = useParams().milestoneID
  const { me, users, actor, impersonation } = useIdentity()
  const [detail, setDetail] = useState<ProjectDetail | null>(null)
  const [memberships, setMemberships] = useState<ProjectMembership[]>([])
  const [error, setError] = useState('')
  const [addingMilestone, setAddingMilestone] = useState(false)
  const [projectDetailsOpen, setProjectDetailsOpen] = useState(false)
  const [editingProject, setEditingProject] = useState(false)
  const [mutationPending, setMutationPending] = useState(false)
  const isPlatformAdmin = actor?.platform_role === 'ADMIN' && !impersonation

  const reload = useCallback(async () => {
    setError('')
    try {
      const [loaded, loadedMemberships] = await Promise.all([
        getProject(number),
        listProjectMembers(number),
      ])
      // Dependent versioned actions may become clickable as soon as reload
      // resolves. Commit the new aggregate versions before returning so a
      // fast follow-up action never submits the stale ETags from the previous
      // render.
      flushSync(() => {
        setDetail(loaded)
        setMemberships(loadedMemberships)
      })
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
  const canManageProject = !impersonation && (
    isPlatformAdmin
    || memberships.some((membership) => (
      membership.user.id === me?.id && membership.active && membership.role === 'admin'
    ))
  )
  const projectUsers = memberships
    .filter((membership) => membership.active)
    .map((membership) => membership.user)
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
                  项目 #{project.number} · 创建者：{project.creator.name}
                </p>
              </div>
              <div className="flex items-center gap-2">
                {canManageProject && (
                  <button
                    type="button"
                    disabled={mutationPending}
                    onClick={() => setEditingProject((value) => !value)}
                    className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:cursor-wait disabled:opacity-50"
                  >
                    编辑项目
                  </button>
                )}
                {canManageProject && (
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
                <textarea
                  name="description"
                  defaultValue={project.description}
                  aria-label="项目说明"
                  rows={3}
                  className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
                />
                <div className="flex justify-end md:col-span-2">
                  <button className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-white">保存</button>
                </div>
              </form>
            )}
            <ProjectMembersPanel
              memberships={memberships}
              users={users}
              canManage={canManageProject}
              pending={mutationPending}
              onAdd={(userID: string, role: ProjectRole) => mutate(() => (
                addProjectMember(project.number, project.version, userID, role)
              ))}
              onChangeRole={(userID: string, role: ProjectRole) => mutate(() => (
                updateProjectMember(project.number, project.version, userID, role)
              ))}
              onRemove={(userID: string) => mutate(() => (
                removeProjectMember(project.number, project.version, userID)
              ))}
            />
            <ProjectAgentConversationsPanel projectNumber={project.number} />
            <ProjectRepositoryAccessPanel
              projectNumber={project.number}
              projectVersion={project.version}
              canManage={canManageProject}
              archived={Boolean(project.archived_at)}
              onChanged={reload}
            />
          </div>
        )}
        {error && <p role="alert" className="border-t border-border py-2 text-sm text-danger">操作失败：{error}</p>}
      </header>

      <div className={cn(
        'flex min-h-0 flex-1 flex-col gap-4 p-4 sm:p-5',
        view === 'workspace' && 'mx-auto w-full max-w-7xl',
      )}>
        {view === 'workspace' && (
          <>
            <Milestones
              detail={detail}
              adding={addingMilestone}
              setAdding={setAddingMilestone}
              users={projectUsers}
              defaultOwnerID={me?.id ?? project.creator.id}
              onMutate={mutate}
              onReload={reload}
            />
            <Backlog detail={detail} users={projectUsers} />
          </>
        )}
        {view === 'milestone' && (
          <Milestones
            detail={detail}
            selected={selectedMilestone}
            adding={addingMilestone}
            setAdding={setAddingMilestone}
            users={projectUsers}
            defaultOwnerID={me?.id ?? project.creator.id}
            onMutate={mutate}
            onReload={reload}
          />
        )}
      </div>
    </div>
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
        aggregateTasks={detail.tasks}
        users={users}
        empty="Backlog 为空。创建任务来记录尚未排入里程碑的工作。"
      />
    </section>
  )
}

function Milestones({
  detail, selected, adding, setAdding, users, defaultOwnerID, onMutate, onReload,
}: {
  detail: ProjectDetail
  selected?: Milestone
  adding: boolean
  setAdding: (value: boolean) => void
  users: UserRef[]
  defaultOwnerID: string
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
  const concludedTasks = selectedTasks.filter((task) => ['done', 'cancelled'].includes(task.phase))
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
            <Link to={`/projects/${project.number}`} className="shrink-0 text-sm text-accent">
              ← 项目交付
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
            aggregateTasks={detail.tasks}
            users={users}
            canCreate={selected.status === 'planned' || selected.status === 'active'}
            empty="还没有任务归入此里程碑。"
          />
        </div>
      </section>
    )
  }

  const currentGroups = [
    ['active', '进行中'],
    ['planned', '计划中'],
  ] as const
  const terminalGroups = [
    ['completed', '已完成'],
    ['cancelled', '已取消'],
  ] as const
  const currentCount = detail.milestones.filter((item) => (
    item.status === 'active' || item.status === 'planned'
  )).length
  const terminalCount = detail.milestones.length - currentCount
  const milestoneCards = (items: Milestone[]) => (
    <div className="grid gap-3 lg:grid-cols-2">
      {[...items].sort((left, right) => left.position - right.position).map((milestone) => {
        const tasks = detail.tasks.filter((task) => (
          !task.archived_at && task.milestone?.id === milestone.id
        ))
        const concluded = tasks.filter((task) => (
          task.phase === 'done' || task.phase === 'cancelled'
        )).length
        const accepted = milestone.acceptance_criteria.filter((criterion) => (
          criterion.current_check?.outcome === 'passed'
          || criterion.current_check?.outcome === 'waived'
        )).length
        return (
          <Link
            key={milestone.id}
            to={`/projects/${project.number}/milestones/${milestone.id}`}
            className="rounded-lg border border-border bg-surface-raised p-4 transition hover:border-accent/40 hover:shadow-[0_4px_16px_rgb(23_43_61/0.06)]"
          >
            <div className="flex items-start justify-between gap-3">
              <h4 className="font-semibold">{milestone.name}</h4>
              <span className="shrink-0 rounded-full bg-accent-subtle px-2 py-1 text-xs font-medium text-accent">
                {MILESTONE_LABELS[milestone.status]}
              </span>
            </div>
            <p className="mt-1 text-sm text-fg-muted">{milestone.outcome}</p>
            <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 text-xs text-fg-muted">
              <span>负责人：{users.find((user) => user.id === milestone.owner_id)?.name ?? '未知'}</span>
              <span>目标：{milestone.target_date ?? '未设置'}</span>
              <span>任务：{concluded}/{tasks.length}</span>
              <span>验收：{accepted}/{milestone.acceptance_criteria.length}</span>
            </div>
          </Link>
        )
      })}
    </div>
  )
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">里程碑</h2>
          <p className="mt-1 text-sm text-fg-muted">当前交付优先显示，已结束的里程碑保留在次级分组中。</p>
        </div>
        <button type="button" onClick={() => setAdding(!adding)} className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">新建里程碑</button>
      </div>
      {adding && (
        <form onSubmit={submit} className="grid gap-3 rounded-lg border border-border bg-surface-raised p-4 md:grid-cols-2">
          <input name="name" required placeholder="里程碑名称" className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          <select name="owner_id" defaultValue={defaultOwnerID} className="rounded-md border border-border-strong bg-surface px-3 py-2">
            {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
          </select>
          <textarea name="outcome" required placeholder="可验证的阶段成果" className="rounded-md border border-border-strong bg-surface px-3 py-2 md:col-span-2" />
          <input name="target_date" type="date" className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          <button className="rounded-md bg-accent px-3 py-2 text-sm text-white">创建</button>
        </form>
      )}
      {currentCount === 0 && (
        <div className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">
          {detail.milestones.length === 0
            ? '还没有里程碑。创建里程碑来规划下一阶段交付。'
            : '当前没有进行中或计划中的里程碑。可以新建里程碑，或从已结束里程碑中重新开启。'}
        </div>
      )}
      {currentGroups.map(([status, label]) => {
        const items = detail.milestones.filter((item) => item.status === status)
        if (!items.length) return null
        return (
          <div key={status}>
            <h3 className="mb-2 text-sm font-medium text-fg-muted">{label} · {items.length}</h3>
            {milestoneCards(items)}
          </div>
        )
      })}
      {terminalCount > 0 && (
        <details className="rounded-lg border border-border bg-surface-subtle/50 px-4 py-3">
          <summary className="cursor-pointer text-sm font-medium text-fg-muted">
            已结束里程碑 · {terminalCount}
          </summary>
          <div className="mt-4 flex flex-col gap-4">
            {terminalGroups.map(([status, label]) => {
              const items = detail.milestones.filter((item) => item.status === status)
              if (!items.length) return null
              return (
                <div key={status}>
                  <h3 className="mb-2 text-sm font-medium text-fg-muted">{label} · {items.length}</h3>
                  {milestoneCards(items)}
                </div>
              )
            })}
          </div>
        </details>
      )}
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
  aggregateTasks,
  users,
  canCreate = true,
  empty,
}: {
  projectNumber: number
  milestoneID?: string
  backlogOnly?: boolean
  aggregateTasks: ProjectDetail['tasks']
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
  const initialTasks = useMemo(() => aggregateTasks
    .filter((task) => (
      !task.archived_at
      && (milestoneID ? task.milestone?.id === milestoneID : true)
      && (backlogOnly ? !task.milestone : true)
    ))
    .sort((left, right) => (
      right.created_at.localeCompare(left.created_at) || right.number - left.number
    )), [aggregateTasks, backlogOnly, milestoneID])
  const initialFilters = useMemo(() => taskFiltersFromSearchParams(searchParams), []) // eslint-disable-line react-hooks/exhaustive-deps
  const initialPageCount = useMemo(() => taskPageCountFromSearchParams(searchParams), []) // eslint-disable-line react-hooks/exhaustive-deps
  const collection = useTaskCollection(query, me?.id, {
    initialFilters,
    initialPageCount,
    initialTasks,
    onFiltersChange: (filters) => {
      setSearchParams(searchParamsWithTaskFilters(searchParams, filters), { replace: true })
    },
    onPageCountChange: (pageCount) => {
      setSearchParams(searchParamsWithTaskPageCount(searchParams, pageCount), { replace: true })
    },
  })
  return (
    <>
      <TaskCollection
        controller={collection}
        tier={tier}
        users={users}
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
    </>
  )
}
