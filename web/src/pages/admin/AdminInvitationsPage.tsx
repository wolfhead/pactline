import { useCallback, useEffect, useRef, useState } from 'react'
import {
  createInvitation,
  listInvitations,
  resendInvitation,
  revokeInvitation,
  rotateInvitationLink,
  searchDirectory,
  type DirectoryPrincipal,
  type Invitation,
} from '@/api/admin-identity'

const STATUS_LABELS: Record<Invitation['status'], string> = {
  pending: '待接受',
  accepted: '已接受',
  expired: '已过期',
  revoked: '已撤销',
}

export default function AdminInvitationsPage() {
  const [invitations, setInvitations] = useState<Invitation[]>([])
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<DirectoryPrincipal[]>([])
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [copyFallback, setCopyFallback] = useState('')
  const searchSequence = useRef(0)

  const load = useCallback(async () => {
    try {
      setInvitations(await listInvitations())
    } catch (reason) {
      setError((reason as Error).message)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const trimmed = query.trim()
    const sequence = ++searchSequence.current
    if (trimmed.length < 2) {
      setResults([])
      setSearching(false)
      return
    }
    setSearching(true)
    const timeout = window.setTimeout(() => {
      searchDirectory(trimmed)
        .then((items) => {
          if (searchSequence.current === sequence) setResults(items.slice(0, 20))
        })
        .catch((reason) => {
          if (searchSequence.current === sequence) setError((reason as Error).message)
        })
        .finally(() => {
          if (searchSequence.current === sequence) setSearching(false)
        })
    }, 300)
    return () => window.clearTimeout(timeout)
  }, [query])

  async function run(action: () => Promise<unknown>, success: string) {
    setError('')
    setNotice('')
    try {
      await action()
      setNotice(success)
      await load()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function copyLink(invitation: Invitation) {
    setCopyFallback('')
    setError('')
    try {
      const { url } = await rotateInvitationLink(invitation.id)
      try {
        await navigator.clipboard.writeText(url)
        setNotice('新邀请链接已复制；旧链接已失效。')
      } catch (clipboardError) {
        console.warn('copy invitation link failed', clipboardError)
        setCopyFallback(url)
        setNotice('已生成新链接；浏览器未允许自动复制，请手动复制。')
      }
      await load()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function invite(principal: DirectoryPrincipal) {
    setError('')
    setNotice('')
    try {
      const invitation = await createInvitation(principal.subject_id)
      setNotice(invitation.delivery?.status === 'failed'
        ? `已创建 ${principal.name} 的邀请，但 Lark 私信发送失败，请复制链接后手动发送。`
        : `已向 ${principal.name} 发送邀请。`)
      await load()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function resend(invitation: Invitation) {
    setError('')
    setNotice('')
    try {
      const result = await resendInvitation(invitation.id)
      setNotice(result.delivery?.status === 'failed'
        ? '邀请已更新，但 Lark 私信仍发送失败，请复制链接后手动发送。'
        : '邀请已重新发送。')
      await load()
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 sm:p-6">
      <header>
        <h1 className="text-xl font-semibold">邀请成员</h1>
        <p className="mt-1 text-sm text-fg-muted">从公司 Lark 通讯录中查找成员并发送一次性邀请。</p>
      </header>

      <section className="rounded-lg border border-border bg-surface-raised p-4">
        <label className="flex flex-col gap-1 text-sm font-medium">
          搜索 Lark 用户
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="输入至少 2 个字符"
            className="mt-1 rounded-md border border-border-strong bg-surface px-3 py-2 font-normal"
          />
        </label>
        {searching && <p className="mt-3 text-sm text-fg-muted">正在搜索…</p>}
        {query.trim().length >= 2 && !searching && results.length === 0 && (
          <p className="mt-3 text-sm text-fg-muted">没有找到匹配用户。</p>
        )}
        {results.length > 0 && (
          <ul className="mt-3 divide-y divide-border rounded-md border border-border">
            {results.map((principal) => (
              <li key={principal.subject_id} className="flex items-center gap-3 p-3">
                {principal.avatar_url ? (
                  <img src={principal.avatar_url} alt="" className="size-9 shrink-0 rounded-full object-cover" />
                ) : (
                  <span className="grid size-9 shrink-0 place-items-center rounded-full bg-accent-subtle font-medium text-accent">
                    {principal.name.slice(0, 1).toUpperCase()}
                  </span>
                )}
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{principal.name}</p>
                  <p className="truncate text-xs text-fg-muted">{principal.email ?? '未提供邮箱'}</p>
                </div>
                <button
                  type="button"
                  onClick={() => void invite(principal)}
                  className="shrink-0 rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-accent-fg"
                >
                  邀请
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {error && <p role="alert" className="text-sm text-danger">操作失败：{error}</p>}
      {notice && <p role="status" className="text-sm text-accent">{notice}</p>}
      {copyFallback && (
        <label className="flex flex-col gap-1 text-sm">
          手动复制邀请链接
          <input
            readOnly
            value={copyFallback}
            onFocus={(event) => event.currentTarget.select()}
            className="rounded-md border border-border-strong bg-surface px-3 py-2 font-mono text-xs"
          />
        </label>
      )}

      <section>
        <h2 className="mb-3 text-base font-semibold">邀请记录</h2>
        {invitations.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">还没有邀请记录。</p>
        ) : (
          <div className="flex flex-col gap-3">
            {invitations.map((invitation) => (
              <article key={invitation.id} className="rounded-lg border border-border bg-surface-raised p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <p className="font-medium">{invitation.target_snapshot.name ?? 'Lark 用户'}</p>
                    <p className="text-xs text-fg-muted">{invitation.target_snapshot.email ?? invitation.target_subject_id}</p>
                  </div>
                  <span className="rounded-full bg-surface-subtle px-2 py-1 text-xs">{STATUS_LABELS[invitation.status]}</span>
                </div>
                <div className="mt-3 text-xs text-fg-muted">
                  有效期至 {new Date(invitation.expires_at).toLocaleString()}
                  {invitation.delivery?.status === 'failed' && (
                    <span className="ml-3 text-danger">Lark 私信发送失败，可复制链接后手动发送</span>
                  )}
                </div>
                {invitation.status === 'pending' && (
                  <div className="mt-3 flex flex-wrap gap-2">
                    <button type="button" onClick={() => void resend(invitation)} className="rounded-md border border-border-strong px-3 py-1.5 text-sm">重新发送</button>
                    <button type="button" onClick={() => void copyLink(invitation)} className="rounded-md border border-border-strong px-3 py-1.5 text-sm">复制新链接</button>
                    <button type="button" onClick={() => void run(() => revokeInvitation(invitation.id), '邀请已撤销。')} className="rounded-md border border-border-strong px-3 py-1.5 text-sm text-danger">撤销</button>
                  </div>
                )}
              </article>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
