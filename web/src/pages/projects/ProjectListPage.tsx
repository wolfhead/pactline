import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { createProject, listProjects, type Project } from '@/api/projects'
import { useIdentity } from '@/identity'

const STATUS_LABELS = {
  planned: '规划中',
  active: '进行中',
  paused: '已暂停',
  completed: '已完成',
  cancelled: '已取消',
} as const

export default function ProjectListPage() {
  const { me, users } = useIdentity()
  const navigate = useNavigate()
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [showArchived, setShowArchived] = useState(false)

  useEffect(() => {
    let cancelled = false
    listProjects(showArchived)
      .then((items) => {
        if (!cancelled) setProjects(items)
      })
      .catch((reason) => {
        if (!cancelled) setError((reason as Error).message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [me?.id, showArchived])

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    setError('')
    try {
      const project = await createProject({
        name: String(data.get('name') ?? ''),
        outcome: String(data.get('outcome') ?? ''),
        owner_id: String(data.get('owner_id') ?? ''),
        target_date: String(data.get('target_date') ?? '') || null,
      })
      navigate(`/projects/${project.number}`)
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">项目</h1>
          <p className="mt-1 text-sm text-fg-muted">以可验收的成果组织任务和里程碑。</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setShowArchived((value) => !value)}
            className="rounded-md border border-border-strong px-3 py-2 text-sm"
          >
            {showArchived ? '隐藏已归档' : '显示已归档'}
          </button>
          <button
            type="button"
            onClick={() => setCreating((value) => !value)}
            className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white"
          >
            新建项目
          </button>
        </div>
      </header>

      {creating && (
        <form onSubmit={handleCreate} className="grid gap-3 rounded-lg border border-border bg-surface-raised p-4 md:grid-cols-2">
          <label className="flex flex-col gap-1 text-sm">
            项目名称
            <input name="name" required className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            负责人
            <select name="owner_id" defaultValue={me?.id} className="rounded-md border border-border-strong bg-surface px-3 py-2">
              {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
            </select>
          </label>
          <label className="flex flex-col gap-1 text-sm md:col-span-2">
            预期成果
            <textarea name="outcome" required rows={3} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            目标日期
            <input name="target_date" type="date" className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          </label>
          <div className="flex items-end justify-end">
            <button type="submit" className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-white">创建</button>
          </div>
        </form>
      )}

      {error && <p role="alert" className="text-sm text-danger">操作失败：{error}</p>}
      {loading ? (
        <p className="text-sm text-fg-muted">正在加载项目…</p>
      ) : projects.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-fg-muted">
          还没有项目。项目用于承载明确成果、负责人和验收标准。
        </div>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {projects.map((project) => {
            const progress = project.eligible_tasks === 0
              ? null
              : Math.round((project.completed_tasks / project.eligible_tasks) * 100)
            return (
              <Link
                key={project.id}
                to={`/projects/${project.number}`}
                className="rounded-lg border border-border bg-surface-raised p-4 hover:border-border-strong"
              >
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="font-mono text-xs text-fg-muted">#{project.number}</p>
                    <h2 className="mt-1 font-semibold">{project.name}</h2>
                  </div>
                  <span className="rounded-full bg-surface-subtle px-2 py-1 text-xs">
                    {project.archived_at ? '已归档' : STATUS_LABELS[project.status]}
                  </span>
                </div>
                <p className="mt-3 line-clamp-2 text-sm text-fg-muted">{project.outcome}</p>
                <div className="mt-4 flex flex-wrap gap-x-4 gap-y-1 text-xs text-fg-muted">
                  <span>负责人：{project.owner.name}</span>
                  <span>任务进度：{progress === null ? '暂无任务' : `${progress}%`}</span>
                  <span>验收：{project.satisfied_criteria}/{project.active_criteria}</span>
                </div>
              </Link>
            )
          })}
        </div>
      )}
    </div>
  )
}
