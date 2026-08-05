import { Clock3, RefreshCw, ShieldX } from 'lucide-react'
import PactlineBrand from '@/components/brand/PactlineBrand'
import { useIdentity } from '@/identity'

export default function AccessDecisionPage() {
  const { actor, refresh, logout } = useIdentity()
  const rejected = actor?.access_status === 'REJECTED'
  const Icon = rejected ? ShieldX : Clock3

  return (
    <main className="grid min-h-dvh place-items-center bg-canvas p-5 text-fg">
      <section className="w-full max-w-md rounded-xl border border-border bg-surface-raised p-6 shadow-sm sm:p-8">
        <PactlineBrand />
        <div className="mt-8 flex items-start gap-4">
          <span className={rejected
            ? 'grid size-11 shrink-0 place-items-center rounded-lg bg-danger-subtle text-danger'
            : 'grid size-11 shrink-0 place-items-center rounded-lg bg-surface-subtle text-priority-medium'}
          >
            <Icon className="size-5" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <h1 className="text-xl font-semibold">
              {rejected ? '访问申请未通过' : '访问申请等待审批'}
            </h1>
            <p className="mt-2 text-sm leading-6 text-fg-muted">
              {rejected
                ? '管理员暂未批准你的 Pactline 访问权限。如需继续使用，请联系系统管理员重新审核。'
                : '你的 Lark 身份已经验证。管理员通过申请后，你可以直接进入 Pactline。'}
            </p>
          </div>
        </div>

        <dl className="mt-7 border-y border-border py-4 text-sm">
          <div className="flex items-center justify-between gap-4">
            <dt className="text-fg-muted">申请账号</dt>
            <dd className="truncate font-medium">{actor?.name}</dd>
          </div>
          <div className="mt-3 flex items-center justify-between gap-4">
            <dt className="text-fg-muted">当前状态</dt>
            <dd className={rejected ? 'text-danger' : 'text-priority-medium'}>
              {rejected ? '未通过' : '等待管理员审批'}
            </dd>
          </div>
        </dl>

        <div className="mt-6 flex flex-col gap-3 sm:flex-row">
          <button
            type="button"
            onClick={() => void refresh()}
            className="inline-flex min-h-10 flex-1 items-center justify-center gap-2 rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
          >
            <RefreshCw className="size-4" aria-hidden="true" />
            重新检查状态
          </button>
          <button
            type="button"
            onClick={() => void logout()}
            className="min-h-10 rounded-md border border-border-strong px-4 py-2 text-sm font-medium hover:bg-surface-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
          >
            退出登录
          </button>
        </div>
      </section>
    </main>
  )
}
