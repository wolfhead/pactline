import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { createProject, listProjects, type Project } from '@/api/projects'
import { useIdentity } from '@/identity'

export default function ProjectListPage() {
  const { me, impersonation } = useIdentity()
  const navigate = useNavigate()
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [showArchived, setShowArchived] = useState(false)


  useEffect(() => {
    let cancelled = false
    setLoading(true)
    listProjects(showArchived)
      .then((items) => { if (!cancelled) setProjects(items) })
      .catch((reason) => { if (!cancelled) setError((reason as Error).message) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [me?.id, showArchived])

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    setError('')
    try {
      const project = await createProject({
        name: String(data.get('name') ?? ''),
        description: String(data.get('description') ?? ''),
      })
      navigate(`/projects/${project.number}/overview`)
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">项目</h1>
          <p className="mt-1 text-sm text-fg-muted">长期工作空间；阶段性交付由里程碑承载。</p>
        </div>
        <div className="flex items-center gap-2">
          <button type="button" data-read-only-allowed="true" onClick={() => setShowArchived((value) => !value)} className="rounded-md border border-border-strong px-3 py-2 text-sm">
            {showArchived ? '隐藏已归档' : '显示已归档'}
          </button>
          {!impersonation && (
            <button type="button" onClick={() => setCreating((value) => !value)} className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">
              新建项目
            </button>
          )}
        </div>
      </header>

      {creating && (
        <form onSubmit={handleCreate} className="grid gap-3 rounded-lg border border-border bg-surface-raised p-4 shadow-sm md:grid-cols-2">
          <label className="flex flex-col gap-1 text-sm">
            项目名称
            <input name="name" required className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          </label>
          <label className="flex flex-col gap-1 text-sm md:col-span-2">
            项目说明
            <textarea name="description" rows={3} className="rounded-md border border-border-strong bg-surface px-3 py-2" />
          </label>
          <div className="flex justify-end md:col-span-2">
            <button type="submit" className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-white">创建</button>
          </div>
        </form>
      )}

      {error && <p role="alert" className="text-sm text-danger">操作失败：{error}</p>}
      {loading ? (
        <p className="text-sm text-fg-muted">正在加载项目…</p>
      ) : projects.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-fg-muted">还没有项目。</div>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {projects.map((project) => (
            <Link key={project.id} to={`/projects/${project.number}/overview`} className="rounded-lg border border-border bg-surface-raised p-4 shadow-[0_1px_3px_rgb(23_43_61/0.05)] transition hover:-translate-y-0.5 hover:border-accent/40 hover:shadow-md">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <p className="font-mono text-xs text-fg-muted">#{project.number}</p>
                  <h2 className="mt-1 font-semibold">{project.name}</h2>
                </div>
                {project.archived_at && <span className="rounded-full bg-surface-subtle px-2 py-1 text-xs">已归档</span>}
              </div>
              <p className="mt-3 line-clamp-2 text-sm text-fg-muted">{project.description || '暂无项目说明'}</p>
              <div className="mt-4 flex gap-4 text-xs text-fg-muted">
                <span>创建者：{project.creator.name}</span>
                <span>任务：{project.completed_tasks}/{project.eligible_tasks}</span>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
