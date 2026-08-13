import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  listAdminAPIActivity,
  listAdminLarkAPIActivity,
  listAdminAPITokens,
  revokeAdminAPIToken,
  type APIAccessEvent,
  type APIAccessFilters,
  type AdminAPIToken,
  type LarkAPIAuditEvent,
  type LarkAPIAuditFilters,
  type LarkAPIOutcome,
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
  includeSuccessfulReads: boolean
}

const EMPTY_FILTERS: FilterForm = {
  userID: '',
  tokenID: '',
  method: '',
  route: '',
  status: '',
  requestID: '',
  includeSuccessfulReads: false,
}

export default function AdminAPIAuditPage() {
  const [source, setSource] = useState<'pactline' | 'lark'>('pactline')
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
        <p className="mt-1 text-sm text-fg-muted">
          {source === 'pactline'
            ? '查看进入 Pactline 的请求。审计日志不保存请求或响应正文。'
            : '查看 Pactline 调用 Lark Open API 的结果和关联信息；不会保存消息、附件、凭证或原始 URL。'}
        </p>
      </header>

      <div className="flex w-fit rounded-lg border border-border bg-surface-subtle p-1" role="tablist" aria-label="API 审计来源">
        <AuditTab active={source === 'pactline'} onClick={() => setSource('pactline')}>Pactline API</AuditTab>
        <AuditTab active={source === 'lark'} onClick={() => setSource('lark')}>Lark API</AuditTab>
      </div>

      {source === 'pactline' ? (
        <>
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
        <label className="flex items-center gap-2 self-end py-2 text-sm">
          <input
            type="checkbox"
            checked={draft.includeSuccessfulReads}
            onChange={(event) => setDraft({ ...draft, includeSuccessfulReads: event.target.checked })}
            className="size-4 accent-accent"
          />
          显示历史成功读取
        </label>
        <div className="flex items-end gap-2">
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
                    <td className="px-4 py-3">
                      {item.token.scopes.includes('work:write')
                        ? '读写'
                        : item.token.scopes.includes('work:execute') ? 'Agent 执行' : '只读'}
                    </td>
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
        </>
      ) : (
        <LarkAPIAuditPanel users={users} />
      )}
    </div>
  )
}

interface LarkFilterForm {
  actorUserID: string
  operation: string
  category: string
  outcome: '' | LarkAPIOutcome
  status: string
  providerRequestID: string
  requestID: string
  agentRunID: string
  eventID: string
}

const EMPTY_LARK_FILTERS: LarkFilterForm = {
  actorUserID: '',
  operation: '',
  category: '',
  outcome: '',
  status: '',
  providerRequestID: '',
  requestID: '',
  agentRunID: '',
  eventID: '',
}

function LarkAPIAuditPanel({ users }: { users: User[] }) {
  const [events, setEvents] = useState<LarkAPIAuditEvent[]>([])
  const [draft, setDraft] = useState<LarkFilterForm>(EMPTY_LARK_FILTERS)
  const [filters, setFilters] = useState<LarkFilterForm>(EMPTY_LARK_FILTERS)
  const [cursor, setCursor] = useState<string | undefined>()
  const [nextCursor, setNextCursor] = useState<string | undefined>()
  const [cursorHistory, setCursorHistory] = useState<Array<string | undefined>>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const userNames = useMemo(
    () => new Map(users.map((user) => [user.id, user.name])),
    [users],
  )

  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    listAdminLarkAPIActivity(toLarkFilters(filters, cursor))
      .then((response) => {
        if (!active) return
        setEvents(response.items)
        setNextCursor(response.next_cursor)
      })
      .catch((reason) => {
        if (active) setError((reason as Error).message)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => { active = false }
  }, [cursor, filters])

  function applyFilters(event: React.FormEvent) {
    event.preventDefault()
    setCursorHistory([])
    setCursor(undefined)
    setFilters({ ...draft })
  }

  return (
    <>
      <form onSubmit={applyFilters} className="grid gap-3 rounded-lg border border-border bg-surface-raised p-4 sm:grid-cols-2 lg:grid-cols-4">
        <FilterSelect label="触发用户" value={draft.actorUserID} onChange={(value) => setDraft({ ...draft, actorUserID: value })}>
          <option value="">全部用户</option>
          {users.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
        </FilterSelect>
        <FilterSelect label="类别" value={draft.category} onChange={(value) => setDraft({ ...draft, category: value })}>
          <option value="">全部类别</option>
          {['identity', 'directory', 'agent', 'notification', 'artifact'].map((value) => <option key={value}>{value}</option>)}
        </FilterSelect>
        <FilterInput label="操作" value={draft.operation} onChange={(value) => setDraft({ ...draft, operation: value })} placeholder="例如：send_notification" />
        <FilterSelect label="结果" value={draft.outcome} onChange={(value) => setDraft({ ...draft, outcome: value as LarkFilterForm['outcome'] })}>
          <option value="">全部结果</option>
          {['succeeded', 'rejected', 'rate_limited', 'unavailable', 'cancelled', 'contract_error'].map((value) => <option key={value}>{value}</option>)}
        </FilterSelect>
        <FilterInput label="HTTP 状态码" value={draft.status} onChange={(value) => setDraft({ ...draft, status: value })} inputMode="numeric" placeholder="例如：429" />
        <FilterInput label="Lark Request ID" value={draft.providerRequestID} onChange={(value) => setDraft({ ...draft, providerRequestID: value })} placeholder="X-Tt-Logid" />
        <FilterInput label="Pactline Request ID" value={draft.requestID} onChange={(value) => setDraft({ ...draft, requestID: value })} placeholder="精确匹配" />
        <FilterInput label="Agent Run ID" value={draft.agentRunID} onChange={(value) => setDraft({ ...draft, agentRunID: value })} placeholder="UUID" />
        <FilterInput label="Event ID" value={draft.eventID} onChange={(value) => setDraft({ ...draft, eventID: value })} placeholder="UUID" />
        <div className="flex items-end gap-2">
          <button type="submit" className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg">筛选</button>
          <button
            type="button"
            onClick={() => {
              setDraft(EMPTY_LARK_FILTERS)
              setFilters(EMPTY_LARK_FILTERS)
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
        <p className="text-sm text-fg-muted">正在加载 Lark API 审计记录…</p>
      ) : events.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border p-6 text-sm text-fg-muted">没有匹配的 Lark API 记录。</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full min-w-[1180px] text-left text-sm">
            <thead className="bg-surface-subtle text-xs text-fg-muted">
              <tr>
                <th className="px-3 py-3 font-medium">时间</th>
                <th className="px-3 py-3 font-medium">操作</th>
                <th className="px-3 py-3 font-medium">接口</th>
                <th className="px-3 py-3 font-medium">结果</th>
                <th className="px-3 py-3 font-medium">耗时 / 大小</th>
                <th className="px-3 py-3 font-medium">关联</th>
                <th className="px-3 py-3 font-medium">Lark Request ID</th>
              </tr>
            </thead>
            <tbody>
              {events.map((event) => (
                <tr key={event.id} className="border-t border-border align-top">
                  <td className="whitespace-nowrap px-3 py-3">{new Date(event.occurred_at).toLocaleString()}</td>
                  <td className="px-3 py-3">
                    <p className="font-medium">{event.operation}</p>
                    <p className="text-xs text-fg-muted">{event.category} · {event.credential_kind}</p>
                  </td>
                  <td className="px-3 py-3 font-mono text-xs">{event.method} {event.route_pattern}</td>
                  <td className="px-3 py-3">
                    <p>{event.outcome}</p>
                    <p className="text-xs text-fg-muted">
                      HTTP {event.http_status ?? '—'} · Lark {event.provider_code ?? '—'}
                    </p>
                    {event.error_category && <p className="text-xs text-danger">{event.error_category}</p>}
                  </td>
                  <td className="whitespace-nowrap px-3 py-3">
                    <p>{event.duration_ms} ms</p>
                    <p className="text-xs text-fg-muted">{event.request_bytes} B → {event.response_bytes} B</p>
                  </td>
                  <td className="px-3 py-3 text-xs">
                    {event.actor_user_id && <p>触发：{userNames.get(event.actor_user_id) ?? event.actor_user_id}</p>}
                    {event.subject_user_id && <p>目标：{userNames.get(event.subject_user_id) ?? event.subject_user_id}</p>}
                    {event.agent_run_id && <p className="font-mono">Run：{event.agent_run_id}</p>}
                    {event.application_event_id && <p className="font-mono">Event：{event.application_event_id}</p>}
                    {event.request_id && <p className="font-mono">Request：{event.request_id}</p>}
                    {!event.actor_user_id && !event.subject_user_id && !event.agent_run_id && !event.application_event_id && !event.request_id && '系统调用'}
                  </td>
                  <td className="px-3 py-3 font-mono text-xs">{event.provider_request_id || '—'}</td>
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
          onClick={() => {
            const previous = cursorHistory[cursorHistory.length - 1]
            setCursorHistory((history) => history.slice(0, -1))
            setCursor(previous)
          }}
          className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:opacity-40"
        >
          上一页
        </button>
        <button
          type="button"
          disabled={!nextCursor || loading}
          onClick={() => {
            if (!nextCursor) return
            setCursorHistory((history) => [...history, cursor])
            setCursor(nextCursor)
          }}
          className="rounded-md border border-border-strong px-3 py-1.5 text-sm disabled:opacity-40"
        >
          下一页
        </button>
      </div>
    </>
  )
}

function toLarkFilters(filters: LarkFilterForm, cursor?: string): LarkAPIAuditFilters {
  const status = filters.status ? Number(filters.status) : undefined
  return {
    actorUserID: filters.actorUserID || undefined,
    operation: filters.operation.trim() || undefined,
    category: filters.category || undefined,
    outcome: filters.outcome || undefined,
    status: Number.isInteger(status) ? status : undefined,
    providerRequestID: filters.providerRequestID.trim() || undefined,
    requestID: filters.requestID.trim() || undefined,
    agentRunID: filters.agentRunID.trim() || undefined,
    eventID: filters.eventID.trim() || undefined,
    cursor,
    pageSize: 50,
  }
}

function AuditTab(props: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={props.active}
      onClick={props.onClick}
      className={`rounded-md px-4 py-2 text-sm font-medium ${props.active ? 'bg-surface-raised text-fg shadow-sm' : 'text-fg-muted hover:text-fg'}`}
    >
      {props.children}
    </button>
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
    importantOnly: !filters.includeSuccessfulReads,
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
