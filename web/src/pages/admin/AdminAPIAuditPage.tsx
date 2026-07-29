import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  listAdminAPIActivity,
  listAdminAPITokens,
  revokeAdminAPIToken,
  type APIAccessEvent,
  type APIAccessFilters,
  type AdminAPIToken,
} from '@/api/access'
import { listAdminUsers } from '@/api/admin-identity'
import type { User } from '@/identity'

interface FilterForm {
  userID: string
  tokenID: string
  method: string
  route: string
  status: string
  requestID: string
}

const EMPTY_FILTERS: FilterForm = {
  userID: '',
  tokenID: '',
  method: '',
  route: '',
  status: '',
  requestID: '',
}

export default function AdminAPIAuditPage() {
  const [events, setEvents] = useState<APIAccessEvent[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [tokens, setTokens] = useState<AdminAPIToken[]>([])
  const [draft, setDraft] = useState<FilterForm>(EMPTY_FILTERS)
  const [filters, setFilters] = useState<FilterForm>(EMPTY_FILTERS)
  const [cursor, setCursor] = useState<string | undefined>()
  const [nextCursor, setNextCursor] = useState<string | undefined>()
  const [cursorHistory, setCursorHistory] = useState<Array<string | undefined>>([])
  const [loading, setLoading] = useState(true)
  const [revokingID, setRevokingID] = useState<string | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([listAdminUsers(), listAdminAPITokens()])
      .then(([userItems, tokenResponse]) => {
        setUsers(userItems)
        setTokens(tokenResponse.items)
      })
      .catch((reason) => setError((reason as Error).message))
  }, [])

  const load = useCallback(async (activeFilters: FilterForm, activeCursor?: string) => {
    setLoading(true)
    setError('')
    try {
      const response = await listAdminAPIActivity(toAPIFilters(activeFilters, activeCursor))
      setEvents(response.items)
      setNextCursor(response.next_cursor)
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(filters, cursor)
  }, [cursor, filters, load])

  const userNames = useMemo(
    () => new Map(users.map((user) => [user.id, user.name])),
    [users],
  )

  function applyFilters(event: React.FormEvent) {
    event.preventDefault()
    setCursorHistory([])
    setCursor(undefined)
    setFilters({ ...draft })
  }

  function nextPage() {
    if (!nextCursor) return
    setCursorHistory((history) => [...history, cursor])
    setCursor(nextCursor)
  }

  function previousPage() {
    if (cursorHistory.length === 0) return
    const previous = cursorHistory[cursorHistory.length - 1]
    setCursorHistory((history) => history.slice(0, -1))
    setCursor(previous)
  }

  async function revokeToken(item: AdminAPIToken) {
    if (!window.confirm(`撤销 ${item.user.name} 的 Token“${item.token.name}”？`)) return
    setRevokingID(item.token.id)
    setError('')
    try {
      await revokeAdminAPIToken(item.token.id)
      const response = await listAdminAPITokens()
      setTokens(response.items)
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setRevokingID(null)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-5 p-4 sm:p-6">
      <header>
        <h1 className="text-xl font-semibold">API 审计</h1>
        <p className="mt-1 text-sm text-fg-muted">查看 Agent API 的认证、路由、结果和幂等重放记录。审计日志不保存请求或响应正文。</p>
      </header>

      <form onSubmit={applyFilters} className="grid gap-3 rounded-lg border border-border bg-surface-raised p-4 sm:grid-cols-2 lg:grid-cols-4">
        <FilterSelect label="用户" value={draft.userID} onChange={(value) => setDraft({ ...draft, userID: value, tokenID: '' })}>
          <option value="">全部用户</option>
          {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
        </FilterSelect>
        <FilterSelect label="Token" value={draft.tokenID} onChange={(value) => setDraft({ ...draft, tokenID: value })}>
          <option value="">全部 Token</option>
          {tokens
            .filter((item) => !draft.userID || item.user.id === draft.userID)
            .map((item) => (
              <option key={item.token.id} value={item.token.id}>
                {item.user.name} · {item.token.name}
              </option>
            ))}
        </FilterSelect>
        <FilterSelect label="方法" value={draft.method} onChange={(value) => setDraft({ ...draft, method: value })}>
          <option value="">全部方法</option>
          {['GET', 'POST', 'PATCH', 'DELETE'].map((method) => <option key={method}>{method}</option>)}
        </FilterSelect>
        <FilterInput label="路由" value={draft.route} onChange={(value) => setDraft({ ...draft, route: value })} placeholder="/api/v1/tasks/{number}" />
        <FilterInput label="状态码" value={draft.status} onChange={(value) => setDraft({ ...draft, status: value })} inputMode="numeric" placeholder="例如：412" />
        <FilterInput label="Request ID" value={draft.requestID} onChange={(value) => setDraft({ ...draft, requestID: value })} placeholder="精确匹配" />
        <div className="flex items-end gap-2 sm:col-span-2">
          <button type="submit" className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg">筛选</button>
          <button
            type="button"
            onClick={() => {
              setDraft(EMPTY_FILTERS)
              setFilters(EMPTY_FILTERS)
              setCursor(undefined)
              setCursorHistory([])
            }}
            className="rounded-md border border-border-strong px-4 py-2 text-sm"
          >
            清空
          </button>
        </div>
      </form>

      {error && <p role="alert" className="text-sm text-danger">加载失败：{error}</p>}
      {loading ? (
        <p className="text-sm text-fg-muted">正在加载审计记录…</p>
      ) : events.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">没有匹配的 API 记录。</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full min-w-[1050px] text-left text-sm">
            <thead className="bg-surface-subtle text-xs text-fg-muted">
              <tr>
                <th className="px-3 py-3 font-medium">时间</th>
                <th className="px-3 py-3 font-medium">用户 / Token</th>
                <th className="px-3 py-3 font-medium">请求</th>
                <th className="px-3 py-3 font-medium">结果</th>
                <th className="px-3 py-3 font-medium">耗时</th>
                <th className="px-3 py-3 font-medium">Request ID</th>
                <th className="px-3 py-3 font-medium">幂等</th>
              </tr>
            </thead>
            <tbody>
              {events.map((event) => (
                <tr key={event.id} className="border-t border-border align-top">
                  <td className="whitespace-nowrap px-3 py-3">{new Date(event.occurred_at).toLocaleString()}</td>
                  <td className="px-3 py-3">
                    <p>{event.user_id ? (userNames.get(event.user_id) ?? event.user_id) : '未认证'}</p>
                    <p className="text-xs text-fg-muted">{event.token_name || event.auth_method}</p>
                  </td>
                  <td className="px-3 py-3 font-mono text-xs">{event.method} {event.route_pattern}</td>
                  <td className="px-3 py-3">
                    <p>{event.status_code}</p>
                    {event.problem_code && <p className="text-xs text-danger">{event.problem_code}</p>}
                  </td>
                  <td className="whitespace-nowrap px-3 py-3">{event.duration_ms} ms</td>
                  <td className="px-3 py-3 font-mono text-xs">{event.request_id}</td>
                  <td className="px-3 py-3">{event.idempotency_replayed ? '重放' : '首次'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="flex justify-end gap-2">
        <button
          type="button"
          disabled={cursorHistory.length === 0 || loading}
          onClick={previousPage}
          className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:opacity-40"
        >
          上一页
        </button>
        <button
          type="button"
          disabled={!nextCursor || loading}
          onClick={nextPage}
          className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:opacity-40"
        >
          下一页
        </button>
      </div>

      <section>
        <h2 className="mb-3 font-semibold">Token 管理</h2>
        {tokens.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">还没有成员创建 API Token。</p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full min-w-[760px] text-left text-sm">
              <thead className="bg-surface-subtle text-xs text-fg-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">用户</th>
                  <th className="px-4 py-3 font-medium">Token</th>
                  <th className="px-4 py-3 font-medium">权限</th>
                  <th className="px-4 py-3 font-medium">有效期至</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {tokens.map((item) => (
                  <tr key={item.token.id} className="border-t border-border">
                    <td className="px-4 py-3">{item.user.name}</td>
                    <td className="px-4 py-3">
                      <p>{item.token.name}</p>
                      <p className="font-mono text-xs text-fg-muted">{item.token.display_prefix}…</p>
                    </td>
                    <td className="px-4 py-3">{item.token.scopes.includes('work:write') ? '读写' : '只读'}</td>
                    <td className="px-4 py-3">{new Date(item.token.expires_at).toLocaleString()}</td>
                    <td className="px-4 py-3">{item.token.revoked_at ? '已撤销' : '有效'}</td>
                    <td className="px-4 py-3 text-right">
                      {!item.token.revoked_at && (
                        <button
                          type="button"
                          disabled={revokingID === item.token.id}
                          onClick={() => void revokeToken(item)}
                          className="rounded-md border border-border-strong px-3 py-1.5 text-danger disabled:opacity-50"
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
    </div>
  )
}

function toAPIFilters(filters: FilterForm, cursor?: string): APIAccessFilters {
  const status = filters.status ? Number(filters.status) : undefined
  return {
    userID: filters.userID || undefined,
    tokenID: filters.tokenID || undefined,
    method: filters.method || undefined,
    route: filters.route.trim() || undefined,
    status: Number.isInteger(status) ? status : undefined,
    requestID: filters.requestID.trim() || undefined,
    cursor,
    pageSize: 50,
  }
}

function FilterInput(props: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder: string
  inputMode?: React.HTMLAttributes<HTMLInputElement>['inputMode']
}) {
  return (
    <label className="flex flex-col gap-1 text-sm font-medium">
      {props.label}
      <input
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
        placeholder={props.placeholder}
        inputMode={props.inputMode}
        className="rounded-md border border-border-strong bg-surface px-3 py-2 font-normal"
      />
    </label>
  )
}

function FilterSelect(props: {
  label: string
  value: string
  onChange: (value: string) => void
  children: React.ReactNode
}) {
  return (
    <label className="flex flex-col gap-1 text-sm font-medium">
      {props.label}
      <select
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
        className="rounded-md border border-border-strong bg-surface px-3 py-2 font-normal"
      >
        {props.children}
      </select>
    </label>
  )
}
