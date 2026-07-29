import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { flushSync } from 'react-dom'
import { Link, NavLink, useParams } from 'react-router-dom'
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
import { archiveTask, createTask, restoreTask, updateTask } from '@/api/tasks'
import {
  checkCriterion,
  removeCriterion,
  updateCriterion,
  type AcceptanceCriterion,
  type AcceptanceOutcome,
} from '@/api/acceptance'
import { ProblemError } from '@/api/v1/client'
import AcceptanceChecklist from '@/components/projects/AcceptanceChecklist'
import TaskList from '@/components/tasks/TaskList'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { useIdentity } from '@/identity'
import type { Task, TaskPatchBody, UserRef } from '@/task-types'
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
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-5 p-4 sm:p-6">
      <header className="rounded-xl border border-border bg-surface-raised p-4 sm:p-5">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <Link to="/projects" className="text-sm text-accent">← 全部项目</Link>
            <p className="mt-3 font-mono text-xs text-fg-muted">项目 #{project.number}</p>
            <h1 className="mt-1 text-xl font-semibold">{project.name}</h1>
            <p className="mt-2 max-w-3xl text-sm text-fg-muted">{project.description || '暂无项目说明'}</p>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-sm text-fg-muted">负责人：{project.owner.name}</span>
            {project.archived_at && <span className="rounded-full bg-surface-subtle px-2 py-1 text-xs">已归档</span>}
            <button
              type="button"
              disabled={mutationPending}
              onClick={() => setEditingProject((value) => !value)}
              className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:cursor-wait disabled:opacity-50"
            >
              编辑
            </button>
            {canAdminister && (
              <button
                type="button"
                disabled={mutationPending}
                onClick={() => void mutate(() => applyProjectArchive(number, project.version, !project.archived_at))}
                className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:cursor-wait disabled:opacity-50"
              >
                {project.archived_at ? '恢复' : '归档'}
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
        <nav aria-label="项目视图" className="mt-5 flex gap-1 border-b border-border">
          {([
            ['overview', '整体视图'],
            ['milestones', '里程碑'],
            ['backlog', 'Backlog'],
          ] as const).map(([key, label]) => (
            <NavLink
              key={key}
              to={`${base}/${key}`}
              className={({ isActive }) => cn(
                '-mb-px border-b-2 px-3 py-2 text-sm',
                isActive ? 'border-accent font-medium text-accent' : 'border-transparent text-fg-muted',
              )}
            >
              {label}
            </NavLink>
          ))}
        </nav>
        {error && <p role="alert" className="mt-3 text-sm text-danger">操作失败：{error}</p>}
      </header>

      {view === 'overview' && <Overview detail={detail} users={users} />}
      {view === 'backlog' && (
        <Backlog
          detail={detail}
          users={users}
          onMutate={mutate}
        />
      )}
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
  onMutate,
}: {
  detail: ProjectDetail
  users: UserRef[]
  onMutate: (operation: () => Promise<unknown>) => Promise<boolean>
}) {
  const [title, setTitle] = useState('')
  const backlog = detail.tasks.filter((task) => !task.milestone && !task.archived_at)
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!title.trim()) return
    const created = await onMutate(() => createTask({
      title: title.trim(),
      project_number: detail.project.number,
    }))
    if (created) setTitle('')
  }
  return (
    <section className="rounded-lg border border-border bg-surface-raised">
      <div className="border-b border-border p-4">
        <h2 className="font-semibold">项目 Backlog</h2>
        <p className="mt-1 text-sm text-fg-muted">尚未安排到里程碑的任务。</p>
        <form onSubmit={submit} className="mt-3 flex gap-2">
          <input value={title} onChange={(event) => setTitle(event.target.value)} aria-label="新建 Backlog 任务" placeholder="输入标题，回车创建…" className="min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-3 py-2 text-sm" />
          <button className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">创建</button>
        </form>
      </div>
      <ProjectTaskCollection tasks={backlog} users={users} onMutate={onMutate} empty="Backlog 为空。" />
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
  const [taskTitle, setTaskTitle] = useState('')
  const [editingMilestone, setEditingMilestone] = useState(false)
  const [acceptanceMutationPending, setAcceptanceMutationPending] = useState(false)
  useEffect(() => {
    setEditingMilestone(false)
    setTaskTitle('')
    setAcceptanceMutationPending(false)
  }, [selected?.id])
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
    const tasks = detail.tasks.filter((task) => task.milestone?.id === selected.id && !task.archived_at)
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
      <section className="flex flex-col gap-4">
        <Link to={`/projects/${project.number}/milestones`} className="text-sm text-accent">← 返回里程碑</Link>
        <div className="rounded-lg border border-border bg-surface-raised p-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <h2 className="text-lg font-semibold">{selected.name}</h2>
              <p className="mt-1 text-sm text-fg-muted">{selected.outcome}</p>
              {selected.description && <p className="mt-2 text-sm text-fg-muted">{selected.description}</p>}
              <p className="mt-2 text-xs text-fg-muted">负责人：{users.find((user) => user.id === selected.owner_id)?.name ?? '未知'} · 目标：{selected.target_date ?? '未设置'}</p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <span className="rounded-full bg-surface-subtle px-2 py-1 text-xs">{MILESTONE_LABELS[selected.status]}</span>
              <button
                type="button"
                onClick={() => setEditingMilestone((value) => !value)}
                className="rounded-md border border-border-strong px-3 py-1.5 text-sm"
              >
                编辑
              </button>
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
          <div className="mt-4 flex gap-2">
            {selected.status === 'planned' && <button disabled={acceptanceMutationPending} onClick={() => void lifecycle('activate')} className="rounded-md bg-accent px-3 py-2 text-sm text-white disabled:cursor-wait disabled:opacity-50">激活</button>}
            {selected.status === 'active' && <button disabled={acceptanceMutationPending} onClick={() => void lifecycle('complete')} className="rounded-md bg-accent px-3 py-2 text-sm text-white disabled:cursor-wait disabled:opacity-50">完成</button>}
            {['planned', 'active'].includes(selected.status) && <button disabled={acceptanceMutationPending} onClick={() => void lifecycle('cancel')} className="rounded-md border border-border-strong px-3 py-2 text-sm disabled:cursor-wait disabled:opacity-50">取消</button>}
            {['completed', 'cancelled'].includes(selected.status) && <button disabled={acceptanceMutationPending} onClick={() => void lifecycle('reopen')} className="rounded-md border border-border-strong px-3 py-2 text-sm disabled:cursor-wait disabled:opacity-50">重新开启</button>}
          </div>
        </div>
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
        <div className="rounded-lg border border-border bg-surface-raised">
          <div className="border-b border-border p-4">
            <h3 className="font-semibold">里程碑任务</h3>
            <form
              className="mt-3 flex gap-2"
              onSubmit={async (event) => {
                event.preventDefault()
                const title = taskTitle.trim()
                if (!title) return
                const created = await onMutate(() => createTask({
                  title,
                  project_number: project.number,
                  milestone_id: selected.id,
                }))
                if (created) setTaskTitle('')
              }}
            >
              <input
                value={taskTitle}
                onChange={(event) => setTaskTitle(event.target.value)}
                aria-label="新建里程碑任务"
                placeholder="输入标题，回车创建…"
                className="min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
              />
              <button className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">创建</button>
            </form>
          </div>
          <ProjectTaskCollection tasks={tasks} users={users} onMutate={onMutate} empty="还没有任务归入此里程碑。" />
        </div>
        <ActivityFeed
          activity={detail.activity.filter((item) => item.milestone_id === selected.id)}
          users={users}
          milestones={detail.milestones}
          title="最近动态"
        />
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
  tasks,
  users,
  onMutate,
  empty,
}: {
  tasks: Task[]
  users: UserRef[]
  onMutate: (operation: () => Promise<unknown>) => Promise<boolean>
  empty: string
}) {
  const tier = useBreakpoint()
  const [items, setItems] = useState(tasks)
  const [rowErrors, setRowErrors] = useState<Record<number, string>>({})
  useEffect(() => setItems(tasks), [tasks])

  function patch(task: Task, body: TaskPatchBody, optimistic: Partial<Task>) {
    const previous = task
    setItems((current) => current.map((item) => (
      item.number === task.number ? { ...item, ...optimistic } : item
    )))
    setRowErrors((current) => ({ ...current, [task.number]: '' }))
    void (async () => {
      let persisted: Task | undefined
      const success = await onMutate(async () => {
        persisted = await updateTask(task.number, task.version, body)
      })
      if (success && persisted) {
        setItems((current) => current.map((item) => (
          item.number === task.number ? persisted! : item
        )))
        return
      }
      setItems((current) => current.map((item) => (
        item.number === task.number ? previous : item
      )))
      setRowErrors((current) => ({
        ...current,
        [task.number]: '更新失败，已恢复原状态。',
      }))
    })()
  }

  function changeArchive(task: Task, archived: boolean) {
    setRowErrors((current) => ({ ...current, [task.number]: '' }))
    void (async () => {
      const success = await onMutate(() => (
        archived
          ? archiveTask(task.number, task.version)
          : restoreTask(task.number, task.version)
      ))
      if (!success) {
        setRowErrors((current) => ({
          ...current,
          [task.number]: `${archived ? '归档' : '恢复'}失败。`,
        }))
      }
    })()
  }

  if (!items.length) return <p className="p-4 text-sm text-fg-muted">{empty}</p>
  return (
    <TaskList
      tasks={items}
      selectedNumber={null}
      tier={tier}
      users={users}
      rowErrors={rowErrors}
      grouped
      onPatch={patch}
      onArchive={(task) => changeArchive(task, true)}
      onRestore={(task) => changeArchive(task, false)}
    />
  )
}
