import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Bot, ChevronRight } from 'lucide-react'
import {
  agentConversationLabel,
  listAgentConversations,
  type AgentConversation,
} from '@/api/agent-conversations'

export default function ProjectAgentConversationsPanel({
  projectNumber,
}: {
  projectNumber: number
}) {
  const [items, setItems] = useState<AgentConversation[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    listAgentConversations()
      .then((conversations) => {
        if (!cancelled) setItems(conversations)
      })
      .catch((reason) => {
        if (!cancelled) setError((reason as Error).message)
      })
    return () => { cancelled = true }
  }, [projectNumber])

  const linked = useMemo(
    () => items.filter((item) => item.default_project?.number === projectNumber),
    [items, projectNumber],
  )

  return (
    <section aria-labelledby="project-agent-conversations-heading" className="mt-5 border-t border-border pt-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 id="project-agent-conversations-heading" className="text-sm font-semibold">关联群聊</h2>
          <p className="mt-1 text-xs text-fg-muted">
            群内 Agent 会优先把未指定项目的事项归入当前项目。
          </p>
        </div>
        <Link
          to={`/agent/conversations?project=${projectNumber}`}
          className="inline-flex items-center gap-1 rounded-md px-2 py-1.5 text-sm font-medium text-accent hover:bg-accent-subtle"
        >
          管理群聊配置
          <ChevronRight className="size-4" aria-hidden="true" />
        </Link>
      </div>

      {error && <p role="alert" className="mt-3 text-sm text-danger">群聊配置加载失败：{error}</p>}
      {!error && linked.length === 0 && (
        <p className="mt-3 rounded-md bg-surface-subtle px-3 py-3 text-sm text-fg-muted">
          暂无群聊绑定到这个项目。
        </p>
      )}
      {linked.length > 0 && (
        <div className="mt-3 divide-y divide-border">
          {linked.map((conversation) => (
            <div key={conversation.id} className="flex min-h-12 items-center gap-3 py-2">
              <span className="grid size-8 shrink-0 place-items-center rounded-md bg-accent-subtle text-accent">
                <Bot className="size-4" aria-hidden="true" />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{agentConversationLabel(conversation)}</p>
                <p className="mt-0.5 text-xs text-fg-muted">
                  {conversation.enabled ? 'Agent 已启用' : 'Agent 已停用'}
                  {' · '}
                  {conversation.binding_active ? '默认绑定生效' : '默认绑定已暂停'}
                </p>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
