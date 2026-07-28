import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { apiGet, setCurrentUserId } from './api/client'

const SEED_PM = '00000000-0000-0000-0000-000000000001'
const STORAGE_KEY = 'bountyboard.currentUserId'

// Mirrors domain.User / domain.UserRole (internal/domain/user.go), the
// account/identity model shared by every part of the product — not part of
// the retired bounty/credit mechanism, so it stays here rather than in the
// (now-deleted) legacy types module.
export type UserRole = 'SPONSOR' | 'ENGINEER' | 'TECH_LEAD' | 'STEWARD'

export interface User {
  id: string
  name: string
  email: string
  roles: UserRole[]
  active: boolean
}

interface IdentityValue {
  me: User | null
  users: User[]
  switchTo: (id: string) => void
}

const IdentityContext = createContext<IdentityValue>({ me: null, users: [], switchTo: () => {} })

export function IdentityProvider({ children }: { children: ReactNode }) {
  const [users, setUsers] = useState<User[]>([])
  const [meId, setMeId] = useState<string>(() => localStorage.getItem(STORAGE_KEY) ?? SEED_PM)
  const [loadError, setLoadError] = useState<string | null>(null)
  // Guards the fallback-and-retry below to exactly one attempt per failure
  // chain, independent of React re-render/commit timing.
  const retriedRef = useRef(false)

  // Setting the module-level current-user id belongs in an effect, not in the
  // render body: render can run multiple times (React StrictMode) or be
  // started and discarded without committing (concurrent rendering), and a
  // module-level mutation isn't safe to repeat or abandon like that. The
  // effect below runs synchronously before the /api/users request is issued,
  // and children stay behind the "loading" gate until that request resolves,
  // so no request ever goes out with a stale or empty X-User-Id.
  //
  // switchTo (below) additionally assigns the module-level id synchronously,
  // ahead of this effect — see the comment there for why.
  useEffect(() => {
    setCurrentUserId(meId)
    localStorage.setItem(STORAGE_KEY, meId)
    let cancelled = false

    apiGet<User[]>('/api/users')
      .then((loaded) => {
        if (cancelled) return
        setUsers(loaded)
        setLoadError(null)
      })
      .catch((err) => {
        // Every backend route, including /api/users itself, sits behind
        // identity middleware. A stale localStorage id (e.g. after `make
        // down` drops the database volume) makes this bootstrap call 401,
        // which would otherwise leave the app stuck on the loading hint
        // forever with no way out except clearing localStorage by hand.
        console.error('load users failed', err)
        if (cancelled) return

        if (!retriedRef.current && meId !== SEED_PM) {
          retriedRef.current = true
          localStorage.removeItem(STORAGE_KEY)
          setMeId(SEED_PM)
          return
        }

        setLoadError('加载用户列表失败,请确认后端服务已启动,然后刷新页面重试。')
      })

    return () => {
      cancelled = true
    }
  }, [meId])

  const me = users.find((u) => u.id === meId) ?? null

  const switchTo = useCallback((id: string) => {
    // Update the module-level current-user id synchronously here, in the
    // event handler, rather than leaving it solely to the effect above.
    // Once `users` is non-empty, children are already mounted, and React
    // fires passive effects child-before-parent within a commit: a child
    // page's effect (tasks 12-15 all add one) can run and issue a request
    // before this provider's own [meId] effect gets a chance to. If that
    // happened, the request would carry the previous identity's header.
    // Assigning here closes that window — do not move this back into the
    // effect, it would reopen it. The effect keeps its own assignment too
    // (for the initial-load and retry paths); both always end up agreeing
    // because they're always set to the same id, so there is no second
    // source of truth.
    setCurrentUserId(id)
    setMeId(id)
  }, [])

  return (
    <IdentityContext.Provider value={{ me, users, switchTo }}>
      {loadError ? (
        // role="alert": this replaces the entire app, so it must announce
        // itself, and it is the only handle a test has on this branch now
        // that there is no .error class to query for.
        <p role="alert" className="p-4 text-sm text-danger">
          {loadError}
        </p>
      ) : users.length === 0 ? (
        <p className="p-4 text-sm text-fg-muted">正在加载用户…</p>
      ) : (
        children
      )}
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
    <label className="flex min-w-0 items-center gap-2 text-xs whitespace-nowrap text-fg-muted">
      当前身份
      {/* Native <select>, matching ThemeToggle beside it — see the note
       * there for why this is not the shadcn one. */}
      <select
        value={me?.id ?? ''}
        onChange={(e) => switchTo(e.target.value)}
        className="min-h-11 min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-2 py-1.5 text-sm text-fg shadow-xs transition-[color,box-shadow] outline-none focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/50 pointer-coarse:min-h-11 sm:min-h-8 sm:flex-none"
      >
        {users.map((u) => (
          <option key={u.id} value={u.id}>
            {u.name}（{u.roles.join(', ')}）
          </option>
        ))}
      </select>
    </label>
  )
}
