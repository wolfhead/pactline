import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { CheckCircle2, GitBranch, KeyRound, PlugZap, ShieldOff } from 'lucide-react'
import {
  createGitLabConnection,
  disableGitLabConnection,
  listGitLabConnections,
  rotateGitLabCredential,
  validateGitLabConnection,
  type GitLabConnection,
} from '@/api/admin-gitlab-connections'

export default function AdminConnectionsPage() {
  const [connections, setConnections] = useState<GitLabConnection[]>([])
  const [label, setLabel] = useState('')
  const [repositoryURL, setRepositoryURL] = useState('')
  const [credential, setCredential] = useState('')
  const [credentialExpiresAt, setCredentialExpiresAt] = useState('')
  const [rotateID, setRotateID] = useState<string | null>(null)
  const [rotateCredential, setRotateCredential] = useState('')
  const [rotateExpiresAt, setRotateExpiresAt] = useState('')
  const [loading, setLoading] = useState(true)
  const [pendingID, setPendingID] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      setConnections(await listGitLabConnections())
      setError('')
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void reload() }, [reload])

  async function create(event: FormEvent) {
    event.preventDefault()
    if (!credential || pendingID) return
    const submittedCredential = credential
    setCredential('')
    setPendingID('create')
    setError('')
    setNotice('')
    try {
      const created = await createGitLabConnection({
        label: label.trim(),
        repository_url: repositoryURL.trim(),
        credential: submittedCredential,
        credential_expires_at: credentialExpiresAt
          ? new Date(credentialExpiresAt).toISOString()
          : null,
      })
      setLabel('')
      setRepositoryURL('')
      setCredentialExpiresAt('')
      setNotice(`已创建 ${created.path_with_namespace} 的 Connection。`)
      await reload()
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setPendingID(null)
    }
  }

  async function run(
    connection: GitLabConnection,
    action: 'validate' | 'disable' | 'rotate',
  ) {
    if (pendingID) return
    setPendingID(connection.id)
    setError('')
    setNotice('')
    try {
      if (action === 'validate') await validateGitLabConnection(connection)
      if (action === 'disable') await disableGitLabConnection(connection)
      if (action === 'rotate') {
        const submittedCredential = rotateCredential
        setRotateCredential('')
        await rotateGitLabCredential(
          connection,
          submittedCredential,
          rotateExpiresAt ? new Date(rotateExpiresAt).toISOString() : null,
        )
        setRotateID(null)
        setRotateExpiresAt('')
      }
      setNotice(action === 'validate' ? '实时鉴权成功。' : action === 'disable' ? 'Connection 已停用。' : '凭证已轮换并重新鉴权。')
      await reload()
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setPendingID(null)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 sm:p-6">
      <header>
        <div className="flex items-center gap-2.5">
          <PlugZap className="size-5 text-accent" aria-hidden="true" />
          <h1 className="text-xl font-semibold">代码仓库 Connection</h1>
        </div>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-fg-muted">
          为一个 GitLab 仓库创建一个机器身份连接。创建时会立即访问仓库鉴权；凭证只写入加密存储，之后不会再次显示。
        </p>
      </header>

      <section aria-labelledby="new-connection-title" className="rounded-lg border border-border bg-surface p-5">
        <h2 id="new-connection-title" className="font-semibold">创建 Connection</h2>
        <form onSubmit={(event) => void create(event)} className="mt-4 grid gap-3 md:grid-cols-2">
          <label className="grid gap-1.5 text-sm font-medium">
            显示名称
            <input required value={label} onChange={(event) => setLabel(event.target.value)} placeholder="Product repository" className="h-10 rounded-md border border-border-strong bg-surface px-3 text-sm outline-none focus:border-accent focus:ring-3 focus:ring-accent/20" />
          </label>
          <label className="grid gap-1.5 text-sm font-medium">
            GitLab 仓库地址
            <input required type="url" value={repositoryURL} onChange={(event) => setRepositoryURL(event.target.value)} placeholder="https://gitlab.example/group/repository" className="h-10 rounded-md border border-border-strong bg-surface px-3 text-sm outline-none focus:border-accent focus:ring-3 focus:ring-accent/20" />
          </label>
          <label className="grid gap-1.5 text-sm font-medium">
            只读 Access Token
            <input required type="password" autoComplete="new-password" value={credential} onChange={(event) => setCredential(event.target.value)} className="h-10 rounded-md border border-border-strong bg-surface px-3 text-sm outline-none focus:border-accent focus:ring-3 focus:ring-accent/20" />
            <span className="text-xs font-normal text-fg-muted">提交后字段立即清空。请使用仅具备读取仓库与 Merge Request 权限的 Token。</span>
          </label>
          <label className="grid gap-1.5 text-sm font-medium">
            凭证到期时间（可选）
            <input type="datetime-local" value={credentialExpiresAt} onChange={(event) => setCredentialExpiresAt(event.target.value)} className="h-10 rounded-md border border-border-strong bg-surface px-3 text-sm outline-none focus:border-accent focus:ring-3 focus:ring-accent/20" />
            <span className="text-xs font-normal text-fg-muted">仅用于管理员识别和轮换计划，不会触发定时任务。</span>
          </label>
          <div className="md:col-span-2">
            <button type="submit" disabled={pendingID !== null || !credential} className="inline-flex h-10 items-center justify-center gap-2 rounded-md bg-accent px-4 text-sm font-medium text-accent-fg disabled:cursor-wait disabled:opacity-50">
              <KeyRound className="size-4" aria-hidden="true" />{pendingID === 'create' ? '正在鉴权…' : '创建并鉴权'}
            </button>
          </div>
        </form>
      </section>

      {error && (
        <div role="alert" className="flex items-center justify-between gap-3 rounded-md bg-danger-subtle px-3 py-2 text-sm text-danger">
          <span>操作失败：{error}</span>
          <button type="button" onClick={() => void reload()} className="shrink-0 rounded-md px-2 py-1 text-xs font-medium hover:bg-danger/10">重试加载</button>
        </div>
      )}
      {notice && <p role="status" className="rounded-md bg-secondary-subtle px-3 py-2 text-sm text-secondary">{notice}</p>}

      <section aria-labelledby="connections-title">
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 id="connections-title" className="font-semibold">已配置的仓库</h2>
          <span className="text-xs text-fg-muted">{connections.length} 个 Connection</span>
        </div>
        {loading ? (
          <p className="text-sm text-fg-muted">正在加载 Connection…</p>
        ) : error && connections.length === 0 ? null : connections.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">尚未配置 GitLab Connection。</div>
        ) : (
          <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface">
            {connections.map((connection) => (
              <li key={connection.id} className="p-4">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
                  <span className="grid size-9 shrink-0 place-items-center rounded-md bg-accent-subtle text-accent">
                    <GitBranch className="size-4" aria-hidden="true" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="font-medium">{connection.label}</h3>
                      <span className={`rounded-full px-2 py-0.5 text-xs ${connection.status === 'active' ? 'bg-secondary-subtle text-secondary' : 'bg-surface-subtle text-fg-muted'}`}>
                        {connection.status === 'active' ? '已启用' : '已停用'}
                      </span>
                    </div>
                    <a href={connection.canonical_web_url} target="_blank" rel="noreferrer" className="mt-1 block truncate text-sm text-accent hover:underline">{connection.path_with_namespace}</a>
                    <p className="mt-1 text-xs text-fg-muted">Project ID {connection.gitlab_project_id} · 默认分支 {connection.default_branch || '未设置'} · 上次鉴权 {new Date(connection.last_validated_at).toLocaleString()} · 凭证到期 {connection.credential_expires_at ? new Date(connection.credential_expires_at).toLocaleString() : '未记录'}</p>
                  </div>
                  {connection.status === 'active' && (
                    <div className="flex shrink-0 flex-wrap gap-1.5">
                      <button type="button" disabled={pendingID !== null} onClick={() => void run(connection, 'validate')} className="inline-flex items-center gap-1.5 rounded-md border border-border-strong px-2.5 py-1.5 text-xs font-medium hover:bg-surface-subtle disabled:opacity-50"><CheckCircle2 className="size-3.5" aria-hidden="true" />验证</button>
                      <button type="button" disabled={pendingID !== null} onClick={() => { setRotateID(connection.id); setRotateCredential(''); setRotateExpiresAt('') }} className="inline-flex items-center gap-1.5 rounded-md border border-border-strong px-2.5 py-1.5 text-xs font-medium hover:bg-surface-subtle disabled:opacity-50"><KeyRound className="size-3.5" aria-hidden="true" />轮换凭证</button>
                      <button type="button" disabled={pendingID !== null} onClick={() => void run(connection, 'disable')} className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium text-danger hover:bg-danger-subtle disabled:opacity-50"><ShieldOff className="size-3.5" aria-hidden="true" />停用</button>
                    </div>
                  )}
                </div>
                {rotateID === connection.id && (
                  <div className="mt-3 grid gap-2 rounded-md bg-surface-subtle p-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,auto)_auto_auto] sm:items-end">
                    <label htmlFor={`rotate-${connection.id}`} className="grid min-w-0 gap-1 text-xs font-medium text-fg-muted">
                      新 Access Token
                      <input id={`rotate-${connection.id}`} type="password" autoComplete="new-password" value={rotateCredential} onChange={(event) => setRotateCredential(event.target.value)} placeholder="输入新的只读 Access Token" className="h-9 min-w-0 rounded-md border border-border-strong bg-surface px-3 text-sm text-fg" />
                    </label>
                    <label htmlFor={`rotate-expiry-${connection.id}`} className="grid gap-1 text-xs font-medium text-fg-muted">
                      凭证到期时间（可选）
                      <input id={`rotate-expiry-${connection.id}`} type="datetime-local" value={rotateExpiresAt} onChange={(event) => setRotateExpiresAt(event.target.value)} className="h-9 rounded-md border border-border-strong bg-surface px-3 text-sm text-fg" />
                    </label>
                    <button type="button" disabled={!rotateCredential || pendingID !== null} onClick={() => void run(connection, 'rotate')} className="h-9 rounded-md bg-accent px-3 text-xs font-medium text-accent-fg disabled:opacity-50">验证并保存</button>
                    <button type="button" onClick={() => { setRotateID(null); setRotateCredential(''); setRotateExpiresAt('') }} className="h-9 rounded-md px-3 text-xs text-fg-muted hover:bg-surface">取消</button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
