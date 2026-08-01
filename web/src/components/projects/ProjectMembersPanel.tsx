import { useMemo, useState, type FormEvent } from 'react'
import { UserPlus } from 'lucide-react'
import type { ProjectMembership, ProjectRole } from '@/api/projects'
import type { UserRef } from '@/task-types'

interface ProjectMembersPanelProps {
  memberships: ProjectMembership[]
  users: UserRef[]
  canManage: boolean
  pending: boolean
  onAdd: (userID: string, role: ProjectRole) => Promise<boolean>
  onChangeRole: (userID: string, role: ProjectRole) => Promise<boolean>
  onRemove: (userID: string) => Promise<boolean>
}

export default function ProjectMembersPanel({
  memberships,
  users,
  canManage,
  pending,
  onAdd,
  onChangeRole,
  onRemove,
}: ProjectMembersPanelProps) {
  const [adding, setAdding] = useState(false)
  const memberIDs = useMemo(
    () => new Set(memberships.map((membership) => membership.user.id)),
    [memberships],
  )
  const candidates = users.filter((user) => !memberIDs.has(user.id))
  const activeAdminCount = memberships.filter(
    (membership) => membership.active && membership.role === 'admin',
  ).length

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const added = await onAdd(
      String(data.get('user_id') ?? ''),
      String(data.get('role') ?? 'member') as ProjectRole,
    )
    if (added) setAdding(false)
  }

  return (
    <section aria-labelledby="project-members-heading" className="mt-5 border-t border-border pt-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 id="project-members-heading" className="text-sm font-semibold">项目成员</h2>
          <p className="mt-0.5 text-xs text-fg-muted">
            {memberships.length} 人可访问此项目；管理员负责设置、成员与归档。
          </p>
        </div>
        {canManage && candidates.length > 0 && (
          <button
            type="button"
            disabled={pending}
            onClick={() => setAdding((value) => !value)}
            className="inline-flex items-center gap-1.5 rounded-md border border-border-strong px-3 py-1.5 text-sm hover:bg-surface-subtle disabled:cursor-wait disabled:opacity-50"
          >
            <UserPlus className="size-4" aria-hidden="true" />
            添加成员
          </button>
        )}
      </div>

      {adding && (
        <form onSubmit={submit} className="mt-3 flex flex-wrap items-end gap-2 rounded-lg bg-surface-subtle p-3">
          <label className="min-w-48 flex-1 text-xs text-fg-muted">
            用户
            <select name="user_id" required className="mt-1 h-9 w-full rounded-md border border-border-strong bg-surface px-2 text-sm text-fg">
              {candidates.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
            </select>
          </label>
          <label className="text-xs text-fg-muted">
            角色
            <select name="role" defaultValue="member" className="mt-1 h-9 rounded-md border border-border-strong bg-surface px-2 text-sm text-fg">
              <option value="member">成员</option>
              <option value="admin">管理员</option>
            </select>
          </label>
          <button disabled={pending} className="h-9 rounded-md bg-accent px-3 text-sm font-medium text-white disabled:cursor-wait disabled:opacity-50">
            确认添加
          </button>
        </form>
      )}

      <div className="mt-3 divide-y divide-border border-y border-border">
        {memberships.map((membership) => {
          const lastActiveAdmin = membership.active
            && membership.role === 'admin'
            && activeAdminCount === 1
          return (
            <div key={membership.id} className="flex min-h-12 items-center gap-3 py-2">
              <span className="grid size-8 shrink-0 place-items-center rounded-full bg-secondary/10 text-xs font-semibold text-secondary" aria-hidden="true">
                {membership.user.name.trim().slice(0, 1).toUpperCase() || '?'}
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{membership.user.name}</p>
                <p className="truncate text-xs text-fg-muted">
                  {membership.user.email ?? (membership.active ? '在职成员' : '账号已停用')}
                </p>
              </div>
              {canManage ? (
                <select
                  aria-label={`${membership.user.name}的项目角色`}
                  value={membership.role}
                  disabled={pending || lastActiveAdmin}
                  onChange={(event) => void onChangeRole(
                    membership.user.id,
                    event.target.value as ProjectRole,
                  )}
                  title={lastActiveAdmin ? '项目必须保留至少一位有效管理员' : undefined}
                  className="h-8 rounded-md border border-border-strong bg-surface px-2 text-xs disabled:cursor-not-allowed disabled:opacity-60"
                >
                  <option value="admin">管理员</option>
                  <option value="member">成员</option>
                </select>
              ) : (
                <span className="rounded-full bg-surface-subtle px-2 py-1 text-xs text-fg-muted">
                  {membership.role === 'admin' ? '管理员' : '成员'}
                </span>
              )}
              {canManage && (
                <button
                  type="button"
                  disabled={pending || lastActiveAdmin}
                  onClick={() => void onRemove(membership.user.id)}
                  title={lastActiveAdmin ? '项目必须保留至少一位有效管理员' : undefined}
                  className="rounded-md px-2 py-1 text-xs text-danger hover:bg-danger/5 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  移除
                </button>
              )}
            </div>
          )
        })}
      </div>
    </section>
  )
}
