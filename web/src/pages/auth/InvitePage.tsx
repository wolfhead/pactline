import { useEffect, useRef, useState } from 'react'
import { apiPost } from '@/api/client'

export default function InvitePage() {
  const started = useRef(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (started.current) return
    started.current = true
    const token = window.location.hash.slice(1)
    window.history.replaceState(null, '', '/invite')
    if (!token) {
      setError('邀请链接无效或已过期。')
      return
    }
    apiPost<{ authorization_url: string }>('/api/invitations/accept', { token })
      .then(({ authorization_url: authorizationURL }) => {
        window.location.assign(authorizationURL)
      })
      .catch((reason) => {
        console.error('accept invitation failed', reason)
        setError('邀请链接无效、已过期或已经使用。请联系管理员重新邀请。')
      })
  }, [])

  return (
    <main className="grid min-h-dvh place-items-center bg-canvas p-5 text-fg">
      <section className="w-full max-w-sm rounded-xl border border-border bg-surface-raised p-6 text-center">
        <h1 className="text-lg font-semibold">加入任务面板</h1>
        {error ? (
          <p role="alert" className="mt-3 text-sm text-danger">{error}</p>
        ) : (
          <p className="mt-3 text-sm text-fg-muted">正在验证邀请并前往 Lark 授权…</p>
        )}
      </section>
    </main>
  )
}
