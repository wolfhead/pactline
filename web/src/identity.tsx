import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { ApiError } from './api/client'
import { v1Get } from './api/v1/client'
import {
  createDevelopmentSession,
  endImpersonation as endImpersonationRequest,
  getMe,
  logout as logoutRequest,
} from './api/identity'

export type UserRole = 'SPONSOR' | 'ENGINEER' | 'TECH_LEAD' | 'STEWARD'
export type PlatformRole = 'ADMIN' | 'MEMBER'
export type AccessStatus = 'PENDING' | 'APPROVED' | 'REJECTED'

export interface User {
  id: string
  name: string
  email: string | null
  avatar_url: string | null
  platform_role: PlatformRole
  access_status: AccessStatus
  roles: UserRole[]
  active: boolean
  created_at: string
  updated_at: string
}

export interface Impersonation {
  id: string
  session_id: string
  actor_user_id: string
  subject_user_id: string
  started_at: string
}

export interface MeResponse {
  actor: User
  subject: User
  impersonation: Impersonation | null
}

type IdentityStatus = 'loading' | 'authenticated' | 'unauthenticated' | 'error'

interface IdentityValue {
  status: IdentityStatus
  actor: User | null
  subject: User | null
  me: User | null
  users: User[]
  impersonation: Impersonation | null
  isReadOnly: boolean
  error: string | null
  refresh: () => Promise<void>
  loginForDevelopment: (userID: string) => Promise<void>
  logout: () => Promise<void>
  endImpersonation: () => Promise<void>
}

const defaultIdentity: IdentityValue = {
  status: 'authenticated',
  actor: null,
  subject: null,
  me: null,
  users: [],
  impersonation: null,
  isReadOnly: false,
  error: null,
  refresh: async () => {},
  loginForDevelopment: async () => {},
  logout: async () => {},
  endImpersonation: async () => {},
}

const IdentityContext = createContext<IdentityValue>(defaultIdentity)

export function IdentityProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<IdentityStatus>('loading')
  const [identity, setIdentity] = useState<MeResponse | null>(null)
  const [users, setUsers] = useState<User[]>([])
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setStatus('loading')
    setError(null)
    try {
      const current = await getMe()
      setIdentity(current)
      setStatus('authenticated')
      if (current.subject.access_status !== 'APPROVED') {
        setUsers([current.subject])
        return
      }
      try {
        const response = await v1Get<{
          items: Array<Pick<
            User,
            'id' | 'name' | 'email' | 'avatar_url' | 'platform_role' | 'active'
          >>
        }>('/api/v1/users')
        setUsers(response.value.items.map((user) => ({
          ...user,
          access_status: 'APPROVED',
          roles: [],
          created_at: '',
          updated_at: '',
        })))
      } catch (usersError) {
        console.error('load user references failed', usersError)
        setUsers([current.subject])
      }
    } catch (reason) {
      setIdentity(null)
      setUsers([])
      if (reason instanceof ApiError && reason.status === 401) {
        setStatus('unauthenticated')
        return
      }
      console.error('load current identity failed', reason)
      setError('无法确认登录状态，请检查服务后重试。')
      setStatus('error')
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const loginForDevelopment = useCallback(async (userID: string) => {
    await createDevelopmentSession(userID)
    await refresh()
  }, [refresh])

  const logout = useCallback(async () => {
    try {
      await logoutRequest()
    } finally {
      setIdentity(null)
      setUsers([])
      setStatus('unauthenticated')
    }
  }, [])

  const endImpersonation = useCallback(async () => {
    await endImpersonationRequest()
    await refresh()
  }, [refresh])

  return (
    <IdentityContext.Provider
      value={{
        status,
        actor: identity?.actor ?? null,
        subject: identity?.subject ?? null,
        me: identity?.subject ?? null,
        users,
        impersonation: identity?.impersonation ?? null,
        isReadOnly: identity?.impersonation !== null && identity?.impersonation !== undefined,
        error,
        refresh,
        loginForDevelopment,
        logout,
        endImpersonation,
      }}
    >
      {children}
    </IdentityContext.Provider>
  )
}

export function useIdentity(): IdentityValue {
  return useContext(IdentityContext)
}
