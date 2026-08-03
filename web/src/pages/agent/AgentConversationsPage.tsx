import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Bot, Check, MessageSquareText, Search } from 'lucide-react'
import {
  agentConversationLabel,
  listAgentConversations,
  updateAgentConversation,
  type AgentConversation,
} from '@/api/agent-conversations'
import { listProjects, type Project } from '@/api/projects'
import { ProblemError } from '@/api/v1/client'
import { useIdentity } from '@/identity'
import { cn } from '@/lib/utils'

interface Draft {
  enabled: boolean
  bindingActive: boolean
  projectNumber: string
  businessContext: string
}

function draftFromConversation(conversation: AgentConversation): Draft {
  return {
    enabled: conversation.enabled,
    bindingActive: conversation.binding_active,
    projectNumber: conversation.default_project?.number.toString() ?? '',
    businessContext: conversation.business_context,
  }
}

function formatSeenAt(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

export default function AgentConversationsPage() {
  const { me, isReadOnly } = useIdentity()
  const [searchParams] = useSearchParams()
  const preferredProjectNumber = Number(searchParams.get('project'))
  const [items, setItems] = useState<AgentConversation[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [selectedID, setSelectedID] = useState('')
  const [draft, setDraft] = useState<Draft | null>(null)
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  async function reload(preferredID?: string) {
    setError('')
    const [conversations, visibleProjects] = await Promise.all([
      listAgentConversations(),
      listProjects(true),
    ])
    setItems(conversations)
    setProjects(visibleProjects)
    const wanted = preferredID ?? selectedID
    const next = conversations.find((item) => item.id === wanted) ?? conversations[0]
    setSelectedID(next?.id ?? '')
    setDraft(next ? draftFromConversation(next) : null)
  }

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    Promise.all([listAgentConversations(), listProjects(true)])
      .then(([conversations, visibleProjects]) => {
        if (cancelled) return
        setItems(conversations)
        setProjects(visibleProjects)
        const preferred = Number.isInteger(preferredProjectNumber)
          ? conversations.find((item) => item.default_project?.number === preferredProjectNumber)
          : undefined
        setSelectedID(preferred?.id ?? conversations[0]?.id ?? '')
      })
      .catch((reason) => {
        if (!cancelled) setError((reason as Error).message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [me?.id, preferredProjectNumber])

  const selected = items.find((item) => item.id === selectedID)
  useEffect(() => {
    const next = items.find((item) => item.id === selectedID)
    setDraft(next ? draftFromConversation(next) : null)
    setSaved(false)
    // Item updates for the same conversation are synchronized explicitly by
    // save/reload so a successful save can keep its visible confirmation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedID])

  const filtered = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase()
    if (!normalized) return items
    return items.filter((item) => [
      agentConversationLabel(item),
      item.external_id,
      item.default_project?.name ?? '',
    ].some((value) => value.toLocaleLowerCase().includes(normalized)))
  }, [items, query])

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected || !draft) return
    setSaving(true)
    setSaved(false)
    setError('')
    try {
      const updated = await updateAgentConversation(selected.id, selected.version, {
        enabled: draft.enabled,
        binding_active: draft.bindingActive,
        business_context: draft.businessContext,
        ...(draft.projectNumber
          ? { default_project_number: Number(draft.projectNumber) }
          : {}),
      })
      setItems((current) => current.map((item) => item.id === updated.id ? updated : item))
      setDraft(draftFromConversation(updated))
      setSaved(true)
    } catch (reason) {
      if (reason instanceof ProblemError && reason.code === 'VERSION_CONFLICT') {
        await reload(selected.id)
        setError('配置已被群内命令或其他管理员更新，已加载最新版本，请重新确认。')
      } else {
        setError((reason as Error).message)
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex min-h-full w-full flex-col">
      <header className="shrink-0 border-b border-border bg-surface px-4 py-4 sm:px-6">
        <div className="mx-auto flex w-full max-w-7xl items-start gap-3">
          <span className="mt-0.5 grid size-9 shrink-0 place-items-center rounded-lg bg-accent-subtle text-accent">
            <Bot className="size-5" aria-hidden="true" />
          </span>
          <div>
            <h1 className="text-xl font-semibold tracking-[-0.02em]">群聊配置</h1>
            <p className="mt-1 max-w-3xl text-sm leading-6 text-fg-muted">
              为每个群设置默认项目和业务背景。明确写在当前消息里的项目始终优先于群默认配置。
            </p>
          </div>
        </div>
      </header>

      <div className="mx-auto grid min-h-0 w-full max-w-7xl flex-1 md:grid-cols-[21rem_minmax(0,1fr)]">
        <aside aria-label="群聊列表" className="border-b border-border bg-sidebar/60 md:border-b-0 md:border-r">
          <div className="border-b border-border p-3">
            <label className="relative block">
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-fg-subtle" aria-hidden="true" />
              <span className="sr-only">搜索群聊</span>
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="搜索群名、项目或群 ID"
                className="w-full rounded-md border border-border-strong bg-surface py-2 pl-9 pr-3 text-sm outline-none focus:border-accent focus:ring-2 focus:ring-accent/15"
              />
            </label>
          </div>

          {loading && <p className="p-4 text-sm text-fg-muted">正在加载群聊配置…</p>}
          {!loading && items.length === 0 && (
            <div className="p-6 text-center">
              <MessageSquareText className="mx-auto size-8 text-fg-subtle" aria-hidden="true" />
              <p className="mt-3 text-sm font-medium">还没有可配置的群聊</p>
              <p className="mt-1 text-xs leading-5 text-fg-muted">在 Lark 群内 @Agent 一次后，这里会出现该群。</p>
            </div>
          )}
          {!loading && items.length > 0 && filtered.length === 0 && (
            <p className="p-4 text-sm text-fg-muted">没有匹配的群聊。</p>
          )}
          <div className="divide-y divide-border">
            {filtered.map((conversation) => (
              <button
                key={conversation.id}
                type="button"
                onClick={() => setSelectedID(conversation.id)}
                className={cn(
                  'w-full px-4 py-3 text-left transition-colors hover:bg-surface',
                  selectedID === conversation.id && 'bg-accent-subtle text-accent',
                )}
              >
                <span className="flex items-start justify-between gap-3">
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-medium text-fg">{agentConversationLabel(conversation)}</span>
                    <span className="mt-1 block truncate text-xs text-fg-muted">
                      {conversation.binding_active && conversation.default_project
                        ? conversation.default_project.name
                        : '未启用默认项目'}
                    </span>
                  </span>
                  <span
                    className={cn(
                      'mt-1 size-2 shrink-0 rounded-full',
                      conversation.enabled ? 'bg-success' : 'bg-fg-subtle',
                    )}
                    title={conversation.enabled ? 'Agent 已启用' : 'Agent 已停用'}
                  />
                </span>
              </button>
            ))}
          </div>
        </aside>

        <main className="min-w-0 p-4 sm:p-6 lg:p-8">
          {error && <p role="alert" className="mb-5 rounded-md bg-danger/10 px-3 py-2 text-sm text-danger">操作失败：{error}</p>}
          {!selected || !draft ? (
            !loading && items.length > 0 && <p className="text-sm text-fg-muted">选择一个群聊查看配置。</p>
          ) : (
            <form onSubmit={handleSave} className="max-w-3xl">
              <div className="flex flex-wrap items-start justify-between gap-4 border-b border-border pb-5">
                <div className="min-w-0">
                  <h2 className="truncate text-lg font-semibold">{agentConversationLabel(selected)}</h2>
                  <p className="mt-1 break-all text-xs text-fg-muted">
                    Lark · {selected.external_id} · 最近活动 {formatSeenAt(selected.last_seen_at)}
                  </p>
                </div>
                <span className="rounded-full bg-surface-subtle px-2.5 py-1 text-xs text-fg-muted">
                  配置版本 {selected.version}
                </span>
              </div>

              <section className="py-6" aria-labelledby="agent-runtime-heading">
                <h3 id="agent-runtime-heading" className="text-sm font-semibold">Agent 运行</h3>
                <p className="mt-1 text-sm text-fg-muted">停用后，普通 @消息不会启动 Agent，查看和修改配置的命令仍可使用。</p>
                <label className="mt-4 flex cursor-pointer items-center justify-between gap-4 rounded-lg bg-surface-subtle px-4 py-3">
                  <span>
                    <span className="block text-sm font-medium">在这个群启用 Agent</span>
                    <span className="mt-0.5 block text-xs text-fg-muted">控制该群能否创建新的工作 Run</span>
                  </span>
                  <input
                    type="checkbox"
                    checked={draft.enabled}
                    disabled={!selected.can_manage || isReadOnly || saving}
                    onChange={(event) => setDraft({ ...draft, enabled: event.target.checked })}
                    className="size-4 accent-accent"
                  />
                </label>
              </section>

              <section className="border-t border-border py-6" aria-labelledby="agent-project-heading">
                <h3 id="agent-project-heading" className="text-sm font-semibold">默认项目</h3>
                <p className="mt-1 text-sm leading-6 text-fg-muted">
                  当前消息没有明确项目时，Agent 才使用这里的默认值。已归档项目不会生效。
                </p>
                <div className="mt-4 grid gap-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
                  <label className="flex min-w-0 flex-col gap-1.5 text-sm font-medium">
                    项目
                    <select
                      value={draft.projectNumber}
                      disabled={!selected.can_manage || isReadOnly || saving}
                      onChange={(event) => setDraft({
                        ...draft,
                        projectNumber: event.target.value,
                        bindingActive: event.target.value ? draft.bindingActive : false,
                      })}
                      className="w-full rounded-md border border-border-strong bg-surface px-3 py-2 text-sm outline-none focus:border-accent focus:ring-2 focus:ring-accent/15"
                    >
                      <option value="">选择项目</option>
                      {projects.map((project) => (
                        <option key={project.id} value={project.number} disabled={Boolean(project.archived_at)}>
                          #{project.number} {project.name}{project.archived_at ? '（已归档）' : ''}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="flex min-h-10 cursor-pointer items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={draft.bindingActive}
                      disabled={!draft.projectNumber || !selected.can_manage || isReadOnly || saving}
                      onChange={(event) => setDraft({ ...draft, bindingActive: event.target.checked })}
                      className="size-4 accent-accent"
                    />
                    启用默认绑定
                  </label>
                </div>
              </section>

              <section className="border-t border-border py-6" aria-labelledby="agent-context-heading">
                <div className="flex items-end justify-between gap-4">
                  <div>
                    <h3 id="agent-context-heading" className="text-sm font-semibold">群业务背景</h3>
                    <p className="mt-1 max-w-2xl text-sm leading-6 text-fg-muted">
                      用 Markdown 写入团队术语、系统边界和稳定约定。它会作为用户提供的背景数据送给 LLM，不能改变权限、工具或系统规则。
                    </p>
                  </div>
                  <span className="shrink-0 text-xs tabular-nums text-fg-muted">{[...draft.businessContext].length}/4000</span>
                </div>
                <textarea
                  aria-label="群业务背景"
                  value={draft.businessContext}
                  maxLength={4000}
                  rows={11}
                  disabled={!selected.can_manage || isReadOnly || saving}
                  onChange={(event) => setDraft({ ...draft, businessContext: event.target.value })}
                  placeholder={'例如：\n- 本群讨论广告投放平台\n- “预发”指每日模型上线前的半小时验证环境\n- 创建任务时默认归入数据平台项目'}
                  className="mt-4 w-full resize-y rounded-lg border border-border-strong bg-surface px-3 py-3 text-sm leading-6 outline-none placeholder:text-fg-subtle focus:border-accent focus:ring-2 focus:ring-accent/15 disabled:bg-surface-subtle"
                />
              </section>

              <div className="sticky bottom-0 flex items-center justify-between gap-4 border-t border-border bg-canvas/95 py-4 backdrop-blur-sm">
                <p className="text-xs text-fg-muted">
                  {selected.can_manage ? '群内命令和网页修改共享同一版本。' : '你可以查看配置，但只有项目管理员可以修改。'}
                </p>
                <div className="flex items-center gap-3">
                  {saved && (
                    <span role="status" className="inline-flex items-center gap-1 text-sm text-success">
                      <Check className="size-4" aria-hidden="true" /> 已保存
                    </span>
                  )}
                  {selected.can_manage && !isReadOnly && (
                    <button
                      type="submit"
                      disabled={saving || Boolean(draft.businessContext.trim() && !draft.projectNumber)}
                      className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg shadow-[0_3px_10px_rgb(37_99_235/0.18)] hover:bg-accent/90 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {saving ? '正在保存…' : '保存配置'}
                    </button>
                  )}
                </div>
              </div>
              {draft.businessContext.trim() && !draft.projectNumber && (
                <p role="status" className="mt-2 text-sm text-warning">先选择项目，再保存群业务背景。</p>
              )}
            </form>
          )}
        </main>
      </div>
    </div>
  )
}
