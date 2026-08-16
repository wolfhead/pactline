import { createContext, type ReactNode, useContext, useEffect, useMemo, useState } from 'react'
import type { Envelope } from './types'

type ConnectionMode = 'connecting' | 'live' | 'polling'
interface LiveState { revision: number; mode: ConnectionMode; lastEventAt?: string }
const LiveContext = createContext<LiveState>({ revision: 0, mode: 'connecting' })

export function LiveObservationProvider({ children }: { children: ReactNode }): JSX.Element {
  const [state, setState] = useState<LiveState>({ revision: 0, mode: 'connecting' })
  useEffect(() => {
    let source: EventSource | undefined
    let polling: number | undefined
    const pulse = (mode: ConnectionMode): void => setState(previous => ({ revision: previous.revision + 1, mode, lastEventAt: new Date().toISOString() }))
    const startPolling = (): void => {
      if (polling !== undefined) return
      pulse('polling')
      polling = window.setInterval(() => pulse('polling'), 5_000)
    }
    if (typeof EventSource === 'undefined') startPolling()
    else {
      source = new EventSource('/api/v1/events')
      source.onopen = () => {
        if (polling !== undefined) { window.clearInterval(polling); polling = undefined }
        setState(previous => ({ ...previous, mode: 'live', lastEventAt: new Date().toISOString() }))
      }
      source.addEventListener('snapshot', () => pulse('live'))
      source.onerror = () => startPolling()
    }
    return () => { source?.close(); if (polling !== undefined) window.clearInterval(polling) }
  }, [])
  return <LiveContext.Provider value={state}>{children}</LiveContext.Provider>
}

export function useLiveObservation(): LiveState { return useContext(LiveContext) }

export function useAPI<T>(path: string): { data?: T; loading: boolean; error?: string; stale: boolean; refresh(): void } {
  const live = useLiveObservation()
  const [nonce, setNonce] = useState(0)
  const [data, setData] = useState<T>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  useEffect(() => {
    const controller = new AbortController()
    setLoading(data === undefined)
    void fetch(path, { signal: controller.signal, headers: { Accept: 'application/json' } }).then(async response => {
      const body = await response.json() as Envelope<T> | { ok: false; error?: { message?: string } }
      if (!response.ok || !body.ok) throw new Error(!body.ok ? body.error?.message ?? `Request failed (${String(response.status)})` : `Request failed (${String(response.status)})`)
      setData(body.data); setError(undefined)
    }).catch(value => {
      if (!controller.signal.aborted) setError(value instanceof Error ? value.message : String(value))
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [path, live.revision, nonce])
  return useMemo(() => ({ data, loading, error, stale: error !== undefined && data !== undefined, refresh: () => setNonce(value => value + 1) }), [data, loading, error])
}
