import { useCallback, useEffect, useState } from 'react'
import { CheckCircle2, FlaskConical, Send } from 'lucide-react'
import {
  listNotificationTestRecipients,
  requestNotificationTest,
  type NotificationTestResult,
} from '@/api/admin-tools'
import type { User } from '@/identity'

export default function AdminToolsPage() {
  const [recipients, setRecipients] = useState<User[]>([])
  const [recipientID, setRecipientID] = useState('')
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<NotificationTestResult | null>(null)

  const loadRecipients = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const available = await listNotificationTestRecipients()
      setRecipients(available)
      setRecipientID((current) => (
        available.some((user) => user.id === current) ? current : (available[0]?.id ?? '')
      ))
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadRecipients()
  }, [loadRecipients])

  async function sendTest() {
    if (!recipientID) return
    setSubmitting(true)
    setError('')
    setResult(null)
    try {
      setResult(await requestNotificationTest(recipientID))
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 sm:p-6">
      <header>
        <div className="flex items-center gap-2.5">
          <FlaskConical className="size-5 text-accent" aria-hidden="true" />
          <h1 className="text-xl font-semibold">测试工具</h1>
        </div>
        <p className="mt-1 max-w-2xl text-sm text-fg-muted">
          运行管理员诊断工具，验证 Pactline 与外部服务之间的真实工作链路。
        </p>
      </header>

      <section aria-labelledby="dm-test-title" className="overflow-hidden rounded-lg border border-border bg-surface">
        <div className="border-b border-border px-5 py-4">
          <div className="flex items-start gap-3">
            <span className="grid size-9 shrink-0 place-items-center rounded-md bg-accent-subtle text-accent">
              <Send className="size-4" aria-hidden="true" />
            </span>
            <div>
              <h2 id="dm-test-title" className="font-semibold">DM 通知链路</h2>
              <p className="mt-0.5 text-sm text-fg-muted">
                创建测试事件，并经过 Outbox、RabbitMQ 和通知消费者向选定用户发送 Lark DM。
              </p>
            </div>
          </div>
        </div>

        <div className="grid gap-5 px-5 py-5 md:grid-cols-[minmax(0,1fr)_minmax(260px,0.8fr)]">
          <div className="flex flex-col gap-4">
            <label className="text-sm font-medium" htmlFor="notification-test-recipient">
              接收用户
            </label>
            <select
              id="notification-test-recipient"
              value={recipientID}
              disabled={loading || recipients.length === 0 || submitting}
              onChange={(event) => setRecipientID(event.target.value)}
              className="h-10 w-full rounded-md border border-border-strong bg-surface px-3 text-sm outline-none focus:border-accent focus:ring-3 focus:ring-accent/20 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {loading && <option value="">正在加载可用用户…</option>}
              {!loading && recipients.length === 0 && <option value="">没有可用用户</option>}
              {recipients.map((user) => (
                <option key={user.id} value={user.id}>
                  {user.name}{user.email ? ` · ${user.email}` : ''}
                </option>
              ))}
            </select>
            <p className="text-xs leading-5 text-fg-muted">
              这里只显示已批准、已启用且具有有效 Lark 身份的用户。
            </p>

            {error && (
              <div role="alert" className="rounded-md bg-danger-subtle px-3 py-2 text-sm text-danger">
                测试请求失败：{error}
              </div>
            )}
            {!loading && recipients.length === 0 && !error && (
              <p role="status" className="rounded-md bg-surface-subtle px-3 py-2 text-sm text-fg-muted">
                当前没有可以接收测试 DM 的用户。请先确认用户已通过审批并完成 Lark 授权。
              </p>
            )}
            {result && (
              <div role="status" className="rounded-md bg-secondary-subtle px-3 py-3 text-sm text-secondary">
                <div className="flex items-center gap-2 font-medium">
                  <CheckCircle2 className="size-4" aria-hidden="true" />
                  测试事件已提交
                </div>
                <p className="mt-1 text-fg-muted">请到接收人的 Lark 私信中确认卡片是否到达。</p>
                <p className="mt-2 break-all font-mono text-xs text-fg-muted">Event ID: {result.event_id}</p>
              </div>
            )}

            <div>
              <button
                type="button"
                disabled={!recipientID || loading || submitting}
                onClick={() => void sendTest()}
                className="inline-flex h-10 items-center justify-center gap-2 rounded-md bg-accent px-4 text-sm font-medium text-accent-fg shadow-sm hover:bg-accent/90 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent/40 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Send className="size-4" aria-hidden="true" />
                {submitting ? '正在提交…' : '发送测试 DM'}
              </button>
            </div>
          </div>

          <aside aria-label="测试消息说明" className="rounded-lg bg-surface-subtle p-4">
            <h3 className="text-sm font-semibold">将发送固定测试卡片</h3>
            <p className="mt-2 text-sm leading-6 text-fg-muted">
              消息会说明通知链路测试已完成，并包含触发管理员、触发时间和 Event ID。
              此工具不支持自定义内容，也不会修改用户或任务数据。
            </p>
            <p className="mt-3 text-xs leading-5 text-fg-muted">
              页面显示“已提交”仅代表事件已写入投递流程；最终送达请以 Lark 实际收到的消息为准。
            </p>
          </aside>
        </div>
      </section>
    </div>
  )
}
