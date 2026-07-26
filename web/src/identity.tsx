import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { apiGet, setCurrentUserId } from './api/client'
import type { User } from './types'

const SEED_PM = '00000000-0000-0000-0000-000000000001'
const STORAGE_KEY = 'bountyboard.currentUserId'

interface IdentityValue {
  me: User | null
  users: User[]
  switchTo: (id: string) => void
}

const IdentityContext = createContext<IdentityValue>({ me: null, users: [], switchTo: () => {} })

export function IdentityProvider({ children }: { children: ReactNode }) {
  const [users, setUsers] = useState<User[]>([])
  const [meId, setMeId] = useState<string>(() => localStorage.getItem(STORAGE_KEY) ?? SEED_PM)

  // Setting the module-level current-user id belongs in an effect, not in the
  // render body: render can run multiple times (React StrictMode) or be
  // started and discarded without committing (concurrent rendering), and a
  // module-level mutation isn't safe to repeat or abandon like that. The
  // effect below runs synchronously before the /api/users request is issued,
  // and children stay behind the "loading" gate until that request resolves,
  // so no request ever goes out with a stale or empty X-User-Id.
  useEffect(() => {
    setCurrentUserId(meId)
    localStorage.setItem(STORAGE_KEY, meId)
    apiGet<User[]>('/api/users')
      .then(setUsers)
      .catch((err) => console.error('load users failed', err))
  }, [meId])

  const me = users.find((u) => u.id === meId) ?? null

  return (
    <IdentityContext.Provider value={{ me, users, switchTo: setMeId }}>
      {users.length === 0 ? <p className="hint">正在加载用户…</p> : children}
    </IdentityContext.Provider>
  )
}

export function useIdentity(): IdentityValue {
  return useContext(IdentityContext)
}

/** UserSwitcher stands in for login during Phase 1 and is removed in Phase 6. */
export function UserSwitcher() {
  const { me, users, switchTo } = useIdentity()
  return (
    <label className="switcher">
      当前身份
      <select value={me?.id ?? ''} onChange={(e) => switchTo(e.target.value)}>
        {users.map((u) => (
          <option key={u.id} value={u.id}>
            {u.name}（{u.roles.join(', ')}）
          </option>
        ))}
      </select>
    </label>
  )
}
