import { useState } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useIdentity } from '@/identity'

const DEVELOPMENT_USERS = [
  { id: '00000000-0000-0000-0000-000000000001', name: 'Primary seed user' },
  { id: '00000000-0000-0000-0000-000000000002', name: 'Seed user 2' },
  { id: '00000000-0000-0000-0000-000000000003', name: 'Seed user 3' },
  { id: '00000000-0000-0000-0000-000000000004', name: 'Seed user 4' },
  { id: '00000000-0000-0000-0000-000000000005', name: 'Seed user 5' },
  { id: '00000000-0000-0000-0000-000000000006', name: 'Seed user 6' },
]

export default function LoginPage() {
  const { status, loginForDevelopment } = useIdentity()
  const location = useLocation()
  const [userID, setUserID] = useState(DEVELOPMENT_USERS[0].id)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const development = import.meta.env.VITE_AUTH_PROVIDER === 'development'

  const from = (location.state as { from?: string } | null)?.from
  if (status === 'authenticated') return <Navigate to={from || '/'} replace />

  async function loginDevelopment() {
    setPending(true)
    setError('')
    try {
      await loginForDevelopment(userID)
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setPending(false)
    }
  }

  return (
    <main className="grid min-h-dvh place-items-center bg-canvas p-5 text-fg">
      <section className="w-full max-w-sm rounded-xl border border-border bg-surface-raised p-6 shadow-sm">
        <h1 className="text-xl font-semibold">登录任务面板</h1>
        <p className="mt-2 text-sm text-fg-muted">使用公司 Lark 账号继续。</p>

        {development ? (
          <div className="mt-6 flex flex-col gap-3">
            <p className="rounded-md bg-danger-subtle p-3 text-xs text-danger">
              Development authentication is enabled.
            </p>
            <label className="flex flex-col gap-1 text-sm">
              Local user
              <select
                value={userID}
                onChange={(event) => setUserID(event.target.value)}
                className="rounded-md border border-border-strong bg-surface px-3 py-2"
              >
                {DEVELOPMENT_USERS.map((user) => (
                  <option key={user.id} value={user.id}>{user.name}</option>
                ))}
              </select>
            </label>
            <button
              type="button"
              disabled={pending}
              onClick={() => void loginDevelopment()}
              className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg disabled:opacity-50"
            >
              {pending ? '正在登录…' : 'Development 登录'}
            </button>
          </div>
        ) : (
          <a
            href="/api/auth/lark/start"
            className="mt-6 flex min-h-11 items-center justify-center rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg"
          >
            使用 Lark 登录
          </a>
        )}
        {error && <p role="alert" className="mt-3 text-sm text-danger">登录失败：{error}</p>}
      </section>
    </main>
  )
}
