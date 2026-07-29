import { useCallback, useEffect, useState } from 'react'
import { Check, Copy, KeyRound, X } from 'lucide-react'
import {
  createAPIToken,
  listAPITokens,
  listOwnAPIActivity,
  revokeAPIToken,
  type APIAccessEvent,
  type APIToken,
  type APITokenScope,
  type IssuedAPIToken,
} from '@/api/access'

type AccessLevel = 'read' | 'write'

export default function APITokensPage() {
  const [tokens, setTokens] = useState<APIToken[]>([])
  const [activity, setActivity] = useState<APIAccessEvent[]>([])
  const [issued, setIssued] = useState<IssuedAPIToken | null>(null)
  const [name, setName] = useState('')
  const [accessLevel, setAccessLevel] = useState<AccessLevel>('write')
  const [expiresInDays, setExpiresInDays] = useState<30 | 90 | 365>(90)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const load = useCallback(async () => {
    setError('')
    try {
      const [tokenResponse, activityResponse] = await Promise.all([
        listAPITokens(),
        listOwnAPIActivity({ pageSize: 20 }),
      ])
      setTokens(tokenResponse.items)
      setActivity(activityResponse.items)
    } catch (reason) {
      setError((reason as Error).message)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function create(event: React.FormEvent) {
    event.preventDefault()
    setPending(true)
    setError('')
    setNotice('')
    try {
      const scopes: APITokenScope[] = accessLevel === 'write'
        ? ['work:write']
        : ['work:read']
      const result = await createAPIToken({
        name: name.trim(),
        scopes,
        expires_in_days: expiresInDays,
      })
      setIssued(result)
      setName('')
      setNotice('Token 已创建。请立即复制；关闭后将无法再次查看完整内容。')
      await load()
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setPending(false)
    }
  }

  async function copyToken() {
    if (!issued) return
    setError('')
    try {
      await navigator.clipboard.writeText(issued.token)
      setNotice('Token 已复制。')
    } catch (reason) {
      console.warn('copy API token failed', reason)
      setError('浏览器未允许自动复制，请从只读输入框手动复制。')
    }
  }

  async function revoke(token: APIToken) {
    if (!window.confirm(`撤销 Token“${token.name}”？使用它的 Agent 将立即无法访问 API。`)) return
    setError('')
    try {
      await revokeAPIToken(token.id)
      setNotice('Token 已撤销。')
      await load()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 sm:p-6">
      <header>
        <h1 className="text-xl font-semibold">API Token</h1>
        <p className="mt-1 text-sm text-fg-muted">
          为 Agent 创建个人访问凭证。Token 继承你的身份，操作会记录到审计日志。
        </p>
      </header>

      {issued && (
        <section aria-labelledby="issued-token-title" className="rounded-lg border border-accent bg-accent-subtle p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <h2 id="issued-token-title" className="font-semibold">立即复制新 Token</h2>
              <p className="mt-1 text-sm text-fg-muted">这是完整 Token 唯一一次显示的机会。</p>
            </div>
            <button
              type="button"
              aria-label="关闭完整 Token"
              onClick={() => setIssued(null)}
              className="rounded-md p-1 text-fg-muted hover:bg-surface/70"
            >
              <X className="size-4" aria-hidden="true" />
            </button>
          </div>
          <div className="mt-3 flex gap-2">
            <input
              aria-label="新 API Token"
              readOnly
              value={issued.token}
              onFocus={(event) => event.currentTarget.select()}
              className="min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-3 py-2 font-mono text-xs"
            />
            <button
              type="button"
              onClick={() => void copyToken()}
              className="inline-flex items-center gap-2 rounded-md bg-accent px-3 py-2 text-sm font-medium text-accent-fg"
            >
              <Copy className="size-4" aria-hidden="true" />
              复制
            </button>
          </div>
        </section>
      )}

      <section className="rounded-lg border border-border bg-surface-raised p-4">
        <div className="mb-4 flex items-center gap-2">
          <KeyRound className="size-5 text-accent" aria-hidden="true" />
          <h2 className="font-semibold">创建 Token</h2>
        </div>
        <form onSubmit={(event) => void create(event)} className="grid gap-4 md:grid-cols-[minmax(12rem,1fr)_auto_auto_auto] md:items-end">
          <label className="flex flex-col gap-1 text-sm font-medium">
            名称
            <input
              required
              maxLength={80}
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="例如：需求整理 Agent"
              className="rounded-md border border-border-strong bg-surface px-3 py-2 font-normal"
            />
          </label>
          <label className="flex flex-col gap-1 text-sm font-medium">
            权限
            <select
              value={accessLevel}
              onChange={(event) => setAccessLevel(event.target.value as AccessLevel)}
              className="rounded-md border border-border-strong bg-surface px-3 py-2 font-normal"
            >
              <option value="read">只读</option>
              <option value="write">读写</option>
            </select>
          </label>
          <label className="flex flex-col gap-1 text-sm font-medium">
            有效期
            <select
              value={expiresInDays}
              onChange={(event) => setExpiresInDays(Number(event.target.value) as 30 | 90 | 365)}
              className="rounded-md border border-border-strong bg-surface px-3 py-2 font-normal"
            >
              <option value={30}>30 天</option>
              <option value={90}>90 天</option>
              <option value={365}>365 天</option>
            </select>
          </label>
          <button
            type="submit"
            disabled={pending || name.trim().length === 0}
            className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg disabled:opacity-50"
          >
            {pending ? '创建中…' : '创建'}
          </button>
        </form>
      </section>

      {error && <p role="alert" className="text-sm text-danger">操作失败：{error}</p>}
      {notice && <p role="status" className="text-sm text-accent">{notice}</p>}

      <section>
        <h2 className="mb-3 font-semibold">我的 Token</h2>
        {tokens.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">还没有 API Token。</p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full min-w-[720px] text-left text-sm">
              <thead className="bg-surface-subtle text-xs text-fg-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">名称</th>
                  <th className="px-4 py-3 font-medium">权限</th>
                  <th className="px-4 py-3 font-medium">最近使用</th>
                  <th className="px-4 py-3 font-medium">有效期至</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {tokens.map((token) => (
                  <tr key={token.id} className="border-t border-border">
                    <td className="px-4 py-3">
                      <p className="font-medium">{token.name}</p>
                      <p className="font-mono text-xs text-fg-muted">{token.display_prefix}…</p>
                    </td>
                    <td className="px-4 py-3">{scopeLabel(token.scopes)}</td>
                    <td className="px-4 py-3">{formatTime(token.last_used_at)}</td>
                    <td className="px-4 py-3">{formatTime(token.expires_at)}</td>
                    <td className="px-4 py-3">{token.revoked_at ? '已撤销' : '有效'}</td>
                    <td className="px-4 py-3 text-right">
                      {!token.revoked_at && (
                        <button
                          type="button"
                          onClick={() => void revoke(token)}
                          className="rounded-md border border-border-strong px-3 py-1.5 text-danger"
                        >
                          撤销
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section>
        <h2 className="mb-3 font-semibold">最近 API 活动</h2>
        {activity.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">还没有 API 使用记录。</p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full min-w-[680px] text-left text-sm">
              <thead className="bg-surface-subtle text-xs text-fg-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">时间</th>
                  <th className="px-4 py-3 font-medium">Token</th>
                  <th className="px-4 py-3 font-medium">请求</th>
                  <th className="px-4 py-3 font-medium">结果</th>
                  <th className="px-4 py-3 font-medium">Request ID</th>
                </tr>
              </thead>
              <tbody>
                {activity.map((event) => (
                  <tr key={event.id} className="border-t border-border">
                    <td className="whitespace-nowrap px-4 py-3">{formatTime(event.occurred_at)}</td>
                    <td className="px-4 py-3">{event.token_name || '—'}</td>
                    <td className="px-4 py-3 font-mono text-xs">{event.method} {event.route_pattern}</td>
                    <td className="px-4 py-3">
                      <span className="inline-flex items-center gap-1">
                        {event.status_code < 400 && <Check className="size-3.5 text-secondary" aria-hidden="true" />}
                        {event.status_code}{event.problem_code ? ` · ${event.problem_code}` : ''}
                      </span>
                    </td>
                    <td className="px-4 py-3 font-mono text-xs">{event.request_id}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}

function scopeLabel(scopes: APITokenScope[]) {
  return scopes.includes('work:write') ? '读写' : '只读'
}

function formatTime(value: string | null) {
  return value ? new Date(value).toLocaleString() : '从未'
}
