import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  applyMilestoneLifecycle,
  applyProjectLifecycle,
  createMilestone,
  createMilestoneCriterion,
  createProjectCriterion,
  getProject,
  updateMilestone,
  updateProject,
  type ProjectDetail,
} from '@/api/projects'
import {
  checkCriterion,
  removeCriterion,
  updateCriterion,
  type AcceptanceCriterion,
  type AcceptanceOutcome,
} from '@/api/acceptance'
import { useIdentity } from '@/identity'
import AcceptanceChecklist from '@/components/projects/AcceptanceChecklist'

const PROJECT_STATUS_LABELS = {
  planned: '规划中',
  active: '进行中',
  paused: '已暂停',
  completed: '已完成',
  cancelled: '已取消',
} as const

const MILESTONE_STATUS_LABELS = {
  open: '进行中',
  completed: '已完成',
  cancelled: '已取消',
} as const

const ACTIVITY_LABELS: Record<string, string> = {
  activated: '激活了项目',
  paused: '暂停了项目',
  completed: '完成了项目',
  cancelled: '取消了项目',
  reopened: '重新开启了项目',
  archived: '归档了项目',
  restored: '恢复了项目',
  milestone_completed: '完成了里程碑',
  milestone_cancelled: '取消了里程碑',
  milestone_reopened: '重新开启了里程碑',
  milestone_updated: '更新了里程碑',
  acceptance_criterion_archived: '归档了验收项',
  acceptance_criterion_removed: '移除了验收项',
  project_name_changed: '修改了项目名称',
  project_outcome_changed: '修改了预期成果',
  project_description_changed: '修改了项目描述',
  project_owner_changed: '修改了项目负责人',
  project_target_date_changed: '修改了目标日期',
}

export default function ProjectDetailPage() {
  const number = Number(useParams().number)
  const { me, users } = useIdentity()
  const [detail, setDetail] = useState<ProjectDetail | null>(null)
  const [error, setError] = useState('')
  const [addingMilestone, setAddingMilestone] = useState(false)
  const [editingProject, setEditingProject] = useState(false)
  const [editingMilestoneID, setEditingMilestoneID] = useState<string | null>(null)

  const reload = useCallback(async () => {
    setError('')
    try {
      setDetail(await getProject(number))
    } catch (reason) {
      setError((reason as Error).message)
    }
  }, [number])

  useEffect(() => {
    void reload()
  }, [reload, me?.id])

  async function addProjectCriterion(criterion: string, instructions: string) {
    await createProjectCriterion(number, {
      criterion,
      verification_instructions: instructions,
      position: detail?.acceptance_criteria.length ?? 0,
    })
    await reload()
  }

  async function recordCheck(
    criterion: AcceptanceCriterion,
    outcome: AcceptanceOutcome,
    evidence: string,
  ) {
    await checkCriterion(criterion.id, criterion.revision, outcome, evidence)
    await reload()
  }

  async function editCriterion(
    criterion: AcceptanceCriterion,
    text: string,
    instructions: string,
  ) {
    await updateCriterion(criterion.id, {
      criterion: text,
      verification_instructions: instructions,
    })
    await reload()
  }

  async function removeAcceptanceCriterion(criterion: AcceptanceCriterion) {
    const needsReason = detail?.project.status === 'active' || detail?.project.status === 'paused'
    const reason = needsReason ? window.prompt('请输入调整验收范围的原因') : undefined
    if (needsReason && !reason) return
    await removeCriterion(criterion.id, reason ?? undefined)
    await reload()
  }

  async function runProjectAction(action: string) {
    setError('')
    try {
      const requiresReason = action === 'reopen'
      const reason = requiresReason ? window.prompt('请输入重新开启原因') : undefined
      if (requiresReason && !reason) return
      await applyProjectLifecycle(number, action, reason ?? undefined)
      await reload()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function submitMilestone(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    setError('')
    try {
      await createMilestone(number, {
        name: String(data.get('name') ?? ''),
        outcome: String(data.get('outcome') ?? ''),
        target_date: String(data.get('target_date') ?? '') || null,
        position: detail?.milestones.length ?? 0,
      })
      setAddingMilestone(false)
      await reload()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function submitProjectEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    setError('')
    try {
      await updateProject(number, {
        name: String(data.get('name') ?? ''),
        outcome: String(data.get('outcome') ?? ''),
        description: String(data.get('description') ?? ''),
        owner_id: String(data.get('owner_id') ?? ''),
        target_date: String(data.get('target_date') ?? '') || null,
      })
      setEditingProject(false)
      await reload()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function submitMilestoneEdit(
    event: FormEvent<HTMLFormElement>,
    milestoneID: string,
  ) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    setError('')
    try {
      await updateMilestone(number, milestoneID, {
        name: String(data.get('name') ?? ''),
        outcome: String(data.get('outcome') ?? ''),
        description: String(data.get('description') ?? ''),
        target_date: String(data.get('target_date') ?? '') || null,
      })
      setEditingMilestoneID(null)
      await reload()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  if (!detail && !error) return <p className="p-6 text-sm text-fg-muted">正在加载项目…</p>
  if (!detail) return <p role="alert" className="p-6 text-sm text-danger">加载失败：{error}</p>

  const { project } = detail
  const progress = project.eligible_tasks === 0
    ? null
    : Math.round((project.completed_tasks / project.eligible_tasks) * 100)

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6">
      <header className="rounded-lg border border-border bg-surface-raised p-4 sm:p-5">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <Link to="/projects" className="text-sm text-accent">← 返回项目</Link>
            <p className="mt-3 font-mono text-xs text-fg-muted">项目 #{project.number}</p>
            <h1 className="mt-1 text-xl font-semibold">{project.name}</h1>
            <p className="mt-2 max-w-3xl text-sm text-fg-muted">{project.outcome}</p>
          </div>
          <span className="rounded-full bg-surface-subtle px-3 py-1.5 text-sm">
            {project.archived_at ? '已归档' : PROJECT_STATUS_LABELS[project.status]}
          </span>
        </div>
        <div className="mt-4 flex flex-wrap gap-x-5 gap-y-2 text-sm text-fg-muted">
          <span>负责人：{project.owner.name}</span>
          <span>目标日期：{project.target_date ?? '未设置'}</span>
          <span>任务进度：{progress === null ? '暂无任务' : `${project.completed_tasks}/${project.eligible_tasks}（${progress}%）`}</span>
          <span>验收：{project.satisfied_criteria}/{project.active_criteria}</span>
        </div>
        <div className="mt-4 flex flex-wrap gap-2">
          <ActionButton onClick={() => setEditingProject((value) => !value)}>编辑</ActionButton>
          {!project.archived_at && project.status === 'planned' && <ActionButton onClick={() => runProjectAction('activate')}>激活项目</ActionButton>}
          {!project.archived_at && project.status === 'active' && <ActionButton onClick={() => runProjectAction('pause')}>暂停</ActionButton>}
          {!project.archived_at && project.status === 'paused' && <ActionButton onClick={() => runProjectAction('activate')}>继续</ActionButton>}
          {!project.archived_at && (project.status === 'active' || project.status === 'paused') && (
            <ActionButton onClick={() => runProjectAction('complete')}>完成项目</ActionButton>
          )}
          {!project.archived_at && (project.status === 'planned' || project.status === 'active' || project.status === 'paused') && (
            <ActionButton onClick={() => runProjectAction('cancel')}>取消项目</ActionButton>
          )}
          {!project.archived_at && (project.status === 'completed' || project.status === 'cancelled') && (
            <ActionButton onClick={() => runProjectAction('reopen')}>重新开启</ActionButton>
          )}
          {!project.archived_at && (project.status === 'completed' || project.status === 'cancelled') && (
            <ActionButton onClick={() => runProjectAction('archive')}>归档</ActionButton>
          )}
          {project.archived_at && <ActionButton onClick={() => runProjectAction('restore')}>恢复</ActionButton>}
        </div>
        {editingProject && (
          <form onSubmit={submitProjectEdit} className="mt-4 grid gap-3 rounded-md bg-surface-subtle p-3 md:grid-cols-2">
            <label className="flex flex-col gap-1 text-sm">
              项目名称
              <input name="name" required defaultValue={project.name} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              负责人
              <select name="owner_id" defaultValue={project.owner.id} className="rounded-md border border-border-strong bg-surface px-3 py-2">
                {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-sm md:col-span-2">
              预期成果
              <textarea name="outcome" required defaultValue={project.outcome} rows={2} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
            </label>
            <label className="flex flex-col gap-1 text-sm md:col-span-2">
              项目说明
              <textarea name="description" defaultValue={project.description} rows={3} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              目标日期
              <input name="target_date" type="date" defaultValue={project.target_date ?? ''} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
            </label>
            <div className="flex items-end justify-end">
              <button type="submit" className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">保存修改</button>
            </div>
          </form>
        )}
        {error && <p role="alert" className="mt-3 text-sm text-danger">操作失败：{error}</p>}
      </header>

      <AcceptanceChecklist
        title="项目验收标准"
        criteria={detail.acceptance_criteria}
        onAdd={addProjectCriterion}
        onCheck={recordCheck}
        onUpdate={editCriterion}
        onRemove={removeAcceptanceCriterion}
      />

      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-lg font-semibold">里程碑</h2>
          <button type="button" onClick={() => setAddingMilestone((value) => !value)} className="text-sm font-medium text-accent">
            添加里程碑
          </button>
        </div>
        {addingMilestone && (
          <form onSubmit={submitMilestone} className="grid gap-3 rounded-lg border border-border bg-surface-raised p-4 md:grid-cols-2">
            <label className="flex flex-col gap-1 text-sm">
              名称
              <input name="name" required className="rounded-md border border-border-strong bg-surface px-3 py-2" />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              目标日期
              <input name="target_date" type="date" className="rounded-md border border-border-strong bg-surface px-3 py-2" />
            </label>
            <label className="flex flex-col gap-1 text-sm md:col-span-2">
              阶段成果
              <textarea name="outcome" required rows={2} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
            </label>
            <div className="flex justify-end md:col-span-2">
              <button type="submit" className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">保存</button>
            </div>
          </form>
        )}
        {detail.milestones.length === 0 && (
          <div className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">
            这个项目暂时不需要里程碑，任务可以直接归入项目。
          </div>
        )}
        {detail.milestones.map((milestone) => {
          const tasks = detail.tasks.filter((task) => task.milestone?.id === milestone.id)
          const eligibleTasks = tasks.filter((task) => task.status !== 'cancelled')
          const completedTasks = eligibleTasks.filter((task) => task.status === 'done').length
          return (
          <article key={milestone.id} className="rounded-lg border border-border bg-surface-raised p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p className="text-xs text-fg-muted">里程碑 {milestone.position + 1}</p>
                <h3 className="mt-1 font-semibold">{milestone.name}</h3>
                <p className="mt-1 text-sm text-fg-muted">{milestone.outcome}</p>
                <p className="mt-2 text-xs text-fg-muted">
                  目标日期：{milestone.target_date ?? '未设置'} · 任务进度：
                  {eligibleTasks.length === 0 ? '暂无任务' : `${completedTasks}/${eligibleTasks.length}`}
                </p>
              </div>
              <span className="rounded-full bg-surface-subtle px-2 py-1 text-xs">
                {MILESTONE_STATUS_LABELS[milestone.status]}
              </span>
            </div>
            <div className="mt-3 flex flex-wrap gap-2">
              <ActionButton onClick={() => setEditingMilestoneID((current) => (
                current === milestone.id ? null : milestone.id
              ))}>编辑</ActionButton>
              {milestone.status === 'open' && (
                <>
                  <ActionButton onClick={async () => {
                    try {
                      await applyMilestoneLifecycle(number, milestone.id, 'complete')
                      await reload()
                    } catch (reason) {
                      setError((reason as Error).message)
                    }
                  }}>完成里程碑</ActionButton>
                  <ActionButton onClick={async () => {
                    try {
                      await applyMilestoneLifecycle(number, milestone.id, 'cancel')
                      await reload()
                    } catch (reason) {
                      setError((reason as Error).message)
                    }
                  }}>取消里程碑</ActionButton>
                </>
              )}
              {(milestone.status === 'completed' || milestone.status === 'cancelled') && (
                <ActionButton onClick={async () => {
                  const reason = window.prompt('请输入重新开启原因')
                  if (!reason) return
                  try {
                    await applyMilestoneLifecycle(number, milestone.id, 'reopen', reason)
                    await reload()
                  } catch (cause) {
                    setError((cause as Error).message)
                  }
                }}>重新开启里程碑</ActionButton>
              )}
            </div>
            {editingMilestoneID === milestone.id && (
              <form
                onSubmit={(event) => submitMilestoneEdit(event, milestone.id)}
                className="mt-4 grid gap-3 rounded-md bg-surface-subtle p-3 md:grid-cols-2"
              >
                <label className="flex flex-col gap-1 text-sm">
                  名称
                  <input name="name" required defaultValue={milestone.name} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
                </label>
                <label className="flex flex-col gap-1 text-sm">
                  目标日期
                  <input name="target_date" type="date" defaultValue={milestone.target_date ?? ''} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
                </label>
                <label className="flex flex-col gap-1 text-sm md:col-span-2">
                  阶段成果
                  <textarea name="outcome" required defaultValue={milestone.outcome} rows={2} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
                </label>
                <label className="flex flex-col gap-1 text-sm md:col-span-2">
                  里程碑说明
                  <textarea name="description" defaultValue={milestone.description} rows={3} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
                </label>
                <div className="flex justify-end md:col-span-2">
                  <button type="submit" className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">保存修改</button>
                </div>
              </form>
            )}
            <div className="mt-4">
              <AcceptanceChecklist
                title="里程碑验收标准"
                criteria={milestone.acceptance_criteria}
                onAdd={async (criterion, instructions) => {
                  await createMilestoneCriterion(number, milestone.id, {
                    criterion,
                    verification_instructions: instructions,
                    position: milestone.acceptance_criteria.length,
                  })
                  await reload()
                }}
                onCheck={recordCheck}
                onUpdate={editCriterion}
                onRemove={removeAcceptanceCriterion}
              />
            </div>
          </article>
          )
        })}
      </section>

      <section className="rounded-lg border border-border bg-surface-raised p-4">
        <h2 className="font-semibold">项目任务</h2>
        {detail.tasks.length === 0 ? (
          <p className="mt-3 text-sm text-fg-muted">还没有任务归入这个项目。</p>
        ) : (
          <div className="mt-3 flex flex-col gap-4">
            {[
              ...detail.milestones.map((milestone) => ({
                key: milestone.id,
                label: milestone.name,
                tasks: detail.tasks.filter((task) => task.milestone?.id === milestone.id),
              })),
              {
                key: 'unscheduled',
                label: '未安排里程碑',
                tasks: detail.tasks.filter((task) => !task.milestone),
              },
            ].filter((group) => group.tasks.length > 0).map((group) => (
              <div key={group.key}>
                <h3 className="text-xs font-medium uppercase tracking-wide text-fg-muted">{group.label}</h3>
                <div className="mt-1 divide-y divide-border">
                  {group.tasks.map((task) => (
                    <Link key={task.id} to={`/tasks/${task.number}`} className="flex items-center gap-3 py-2 text-sm hover:text-accent">
                      <span className="font-mono text-xs text-fg-muted">#{task.number}</span>
                      <span className="min-w-0 flex-1 truncate">{task.title}</span>
                      <span className="text-xs text-fg-muted">{task.status}</span>
                    </Link>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      <section aria-label="项目历史记录" className="rounded-lg border border-border bg-surface-raised p-4">
        <h2 className="font-semibold">历史记录</h2>
        {detail.activity.length === 0 ? (
          <p className="mt-3 text-sm text-fg-muted">还没有项目级记录。</p>
        ) : (
          <ul className="mt-3 flex flex-col gap-2">
            {[...detail.activity].reverse().map((item) => {
              const actor = users.find((user) => user.id === item.actor_id)?.name ?? '某用户'
              return (
                <li key={item.id} className="text-sm">
                  {actor} {ACTIVITY_LABELS[item.action] ?? item.action}
                  {item.reason ? `：${item.reason}` : ''}
                  <span className="text-fg-muted"> · {new Date(item.created_at).toLocaleString()}</span>
                </li>
              )
            })}
          </ul>
        )}
      </section>
    </div>
  )
}

function ActionButton({ children, onClick }: { children: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded-md border border-border-strong px-3 py-1.5 text-sm hover:bg-surface-subtle"
    >
      {children}
    </button>
  )
}
