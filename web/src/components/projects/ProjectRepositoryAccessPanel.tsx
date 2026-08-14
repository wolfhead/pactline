import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { GitBranch, Link2, Unlink } from 'lucide-react'
import {
  bindProjectRepository,
  listProjectRepositories,
  unbindProjectRepository,
  type ProjectRepository,
} from '@/api/project-repositories'
import { ProblemError } from '@/api/v1/client'

interface Props {
  projectNumber: number
  projectVersion: number
  canManage: boolean
  archived: boolean
  onChanged: () => Promise<void>
}

export default function ProjectRepositoryAccessPanel({
  projectNumber,
  projectVersion,
  canManage,
  archived,
  onChanged,
}: Props) {
  const [repositories, setRepositories] = useState<ProjectRepository[]>([])
  const [repositoryURL, setRepositoryURL] = useState('')
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      setRepositories(await listProjectRepositories(projectNumber))
      setError('')
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setLoading(false)
    }
  }, [projectNumber])

  useEffect(() => { void reload() }, [reload])

  async function bind(event: FormEvent) {
    event.preventDefault()
    if (!repositoryURL.trim() || pending) return
    setPending(true)
    setError('')
    try {
      await bindProjectRepository(projectNumber, projectVersion, repositoryURL.trim())
      setRepositoryURL('')
      await Promise.all([reload(), onChanged()])
    } catch (reason) {
      if (reason instanceof ProblemError && reason.code === 'VERSION_CONFLICT') {
        await Promise.all([reload(), onChanged()])
        setError('项目已被其他用户或 Agent 更新，请重新提交。')
      } else {
        setError((reason as Error).message)
      }
    } finally {
      setPending(false)
    }
  }

  async function unbind(repository: ProjectRepository) {
    if (pending) return
    setPending(true)
    setError('')
    try {
      await unbindProjectRepository(projectNumber, projectVersion, repository.id)
      await Promise.all([reload(), onChanged()])
    } catch (reason) {
      if (reason instanceof ProblemError && reason.code === 'VERSION_CONFLICT') {
        await Promise.all([reload(), onChanged()])
        setError('项目已被其他用户或 Agent 更新，请重试。')
      } else {
        setError((reason as Error).message)
      }
    } finally {
      setPending(false)
    }
  }

  return (
    <section aria-labelledby="project-repositories-title" className="mt-5 border-t border-border pt-5">
      <div className="flex items-start gap-3">
        <span className="grid size-8 shrink-0 place-items-center rounded-md bg-accent-subtle text-accent">
          <GitBranch className="size-4" aria-hidden="true" />
        </span>
        <div>
          <h3 id="project-repositories-title" className="text-sm font-semibold">代码仓库授权</h3>
          <p className="mt-0.5 text-xs leading-5 text-fg-muted">
			绑定后，本项目的执行者可以把该仓库中的 Pull Request / Merge Request 关联为交付证据。
          </p>
        </div>
      </div>

      {canManage && !archived && (
        <form onSubmit={(event) => void bind(event)} className="mt-3 flex flex-col gap-2 sm:flex-row">
		  <label htmlFor="project-repository-url" className="sr-only">代码仓库地址</label>
          <input
            id="project-repository-url"
            type="url"
            required
            value={repositoryURL}
            onChange={(event) => setRepositoryURL(event.target.value)}
            placeholder="https://github.com/owner/repository 或 GitLab 仓库地址"
            className="h-10 min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-3 text-sm outline-none focus:border-accent focus:ring-3 focus:ring-accent/20"
          />
          <button
            type="submit"
            disabled={pending || !repositoryURL.trim()}
            className="inline-flex h-10 items-center justify-center gap-2 rounded-md bg-accent px-4 text-sm font-medium text-accent-fg disabled:cursor-wait disabled:opacity-50"
          >
            <Link2 className="size-4" aria-hidden="true" />
            {pending ? '正在鉴权…' : '绑定并鉴权'}
          </button>
        </form>
      )}

      {error && (
        <div role="alert" className="mt-3 flex items-center justify-between gap-3 rounded-md bg-danger-subtle px-3 py-2 text-sm text-danger">
          <span>仓库授权操作失败：{error}</span>
          <button type="button" onClick={() => void reload()} className="shrink-0 rounded-md px-2 py-1 text-xs font-medium hover:bg-danger/10">重试加载</button>
        </div>
      )}
      {loading ? (
        <p className="mt-3 text-sm text-fg-muted">正在加载仓库授权…</p>
      ) : error && repositories.length === 0 ? null : repositories.length === 0 ? (
        <p className="mt-3 rounded-md bg-surface-subtle px-3 py-3 text-sm text-fg-muted">
          尚未绑定代码仓库。系统管理员需要先为仓库创建 Connection。
        </p>
      ) : (
        <ul className="mt-3 divide-y divide-border rounded-lg border border-border bg-surface">
          {repositories.map((repository) => (
            <li key={repository.id} className="flex items-center gap-3 px-3 py-3">
              <GitBranch className="size-4 shrink-0 text-fg-subtle" aria-hidden="true" />
              <div className="min-w-0 flex-1">
                <a href={repository.canonical_web_url} target="_blank" rel="noreferrer" className="block truncate text-sm font-medium text-accent hover:underline">
                  {repository.path_with_namespace}
                </a>
                <p className="mt-0.5 truncate text-xs text-fg-muted">
                  {repository.label} · 默认分支 {repository.default_branch || '未设置'}
                </p>
              </div>
              {canManage && !archived && (
                <button
                  type="button"
                  disabled={pending}
                  onClick={() => void unbind(repository)}
                  className="inline-flex shrink-0 items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium text-danger hover:bg-danger-subtle disabled:opacity-50"
                >
                  <Unlink className="size-3.5" aria-hidden="true" />取消绑定
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
