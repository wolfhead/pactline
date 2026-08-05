import { useCallback, useEffect, useState } from 'react'
import {
  listAdminUsers,
  setUserAccessStatus,
  setUserActive,
  startImpersonation,
} from '@/api/admin-identity'
import { useIdentity, type AccessStatus, type User } from '@/identity'

export default function AdminUsersPage() {
  const { actor, refresh } = useIdentity()
  const [users, setUsers] = useState<User[]>([])
  const [error, setError] = useState('')
  const [pendingID, setPendingID] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError('')
    try {
      setUsers(await listAdminUsers())
    } catch (reason) {
      setError((reason as Error).message)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function toggleActive(user: User) {
    setPendingID(user.id)
    try {
      await setUserActive(user.id, !user.active)
      await load()
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setPendingID(null)
    }
  }

  async function decideAccess(user: User, accessStatus: AccessStatus) {
    setPendingID(user.id)
    try {
      await setUserAccessStatus(user.id, accessStatus)
      await load()
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setPendingID(null)
    }
  }

  async function impersonate(user: User) {
    if (!window.confirm(`将以 ${user.name} 的身份只读查看系统，是否继续？`)) return
    setPendingID(user.id)
    try {
      await startImpersonation(user.id)
      await refresh()
      window.location.assign('/')
    } catch (reason) {
      setError((reason as Error).message)
      setPendingID(null)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-5 p-4 sm:p-6">
      <header>
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-xl font-semibold">用户与访问申请</h1>
          {users.some((user) => user.access_status === 'PENDING') && (
            <span className="rounded-full bg-surface-subtle px-2.5 py-1 text-xs font-medium text-priority-medium">
              {users.filter((user) => user.access_status === 'PENDING').length} 项待处理
            </span>
          )}
        </div>
        <p className="mt-1 text-sm text-fg-muted">审核新成员的系统访问权限，并管理已通过成员的账号状态。</p>
      </header>
      {error && <p role="alert" className="text-sm text-danger">操作失败：{error}</p>}
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="w-full min-w-[640px] text-left text-sm">
          <thead className="bg-surface-subtle text-xs text-fg-muted">
            <tr>
              <th className="px-4 py-3 font-medium">用户</th>
              <th className="px-4 py-3 font-medium">角色</th>
              <th className="px-4 py-3 font-medium">状态</th>
              <th className="px-4 py-3 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <tr key={user.id} className="border-t border-border">
                <td className="px-4 py-3">
                  <div className="flex items-center gap-3">
                    <Avatar user={user} />
                    <div>
                      <p className="font-medium">{user.name}</p>
                      <p className="text-xs text-fg-muted">{user.email ?? '未提供邮箱'}</p>
                    </div>
                  </div>
                </td>
                <td className="px-4 py-3">{user.platform_role === 'ADMIN' ? '管理员' : '成员'}</td>
                <td className="px-4 py-3"><AccessStatusLabel user={user} /></td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-2">
                    {user.access_status === 'PENDING' && (
                      <>
                        <button
                          type="button"
                          disabled={pendingID === user.id}
                          onClick={() => void decideAccess(user, 'APPROVED')}
                          className="rounded-md bg-accent px-3 py-1.5 font-medium text-accent-fg disabled:opacity-50"
                        >
                          通过
                        </button>
                        <button
                          type="button"
                          disabled={pendingID === user.id}
                          onClick={() => void decideAccess(user, 'REJECTED')}
                          className="rounded-md border border-danger/40 px-3 py-1.5 text-danger hover:bg-danger-subtle disabled:opacity-50"
                        >
                          拒绝
                        </button>
                      </>
                    )}
                    {user.access_status === 'REJECTED' && (
                      <button
                        type="button"
                        disabled={pendingID === user.id}
                        onClick={() => void decideAccess(user, 'APPROVED')}
                        className="rounded-md bg-accent px-3 py-1.5 font-medium text-accent-fg disabled:opacity-50"
                      >
                        重新通过
                      </button>
                    )}
                    {user.platform_role === 'MEMBER' && user.access_status === 'APPROVED' && user.active && (
                      <button
                        type="button"
                        disabled={pendingID === user.id}
                        onClick={() => void impersonate(user)}
                        className="rounded-md border border-border-strong px-3 py-1.5 disabled:opacity-50"
                      >
                        只读查看
                      </button>
                    )}
                    {user.id !== actor?.id && user.access_status === 'APPROVED' && (
                      <button
                        type="button"
                        disabled={pendingID === user.id}
                        onClick={() => void toggleActive(user)}
                        className="rounded-md border border-border-strong px-3 py-1.5 disabled:opacity-50"
                      >
                        {user.active ? '停用' : '启用'}
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function AccessStatusLabel({ user }: { user: User }) {
  if (user.access_status === 'PENDING') {
    return <span className="text-priority-medium">等待审批</span>
  }
  if (user.access_status === 'REJECTED') {
    return <span className="text-danger">未通过</span>
  }
  return <span className={user.active ? 'text-success' : 'text-fg-muted'}>{user.active ? '正常' : '已停用'}</span>
}

function Avatar({ user }: { user: User }) {
  return user.avatar_url ? (
    <img src={user.avatar_url} alt="" className="size-9 rounded-full object-cover" />
  ) : (
    <span className="grid size-9 place-items-center rounded-full bg-accent-subtle font-medium text-accent">
      {user.name.slice(0, 1).toUpperCase()}
    </span>
  )
}
